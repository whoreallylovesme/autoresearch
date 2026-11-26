package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	migrations "github.com/whoreallylovesme/autoresearch/migrations"
	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func Open(path string) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLite{db: db}
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL", "PRAGMA synchronous = FULL"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure SQLite: %w", err)
		}
	}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLite) DB() *sql.DB { return s.db }

func (s *SQLite) Close() error { return s.db.Close() }

func (s *SQLite) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)"); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		var applied string
		err := s.db.QueryRowContext(ctx, "SELECT version FROM schema_migrations WHERE version = ?", entry.Name()).Scan(&applied)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		content, err := fs.ReadFile(migrations.FS, entry.Name())
		if err != nil {
			return err
		}
		if err := s.transaction(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(content)); err != nil {
				return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)", entry.Name(), time.Now().UTC().UnixNano())
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) saveEntityWithEvent(ctx context.Context, saveEntity func(executor) error, event *Event) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := saveEntity(tx); err != nil {
			return err
		}
		if event != nil {
			return saveEvent(ctx, tx, *event)
		}
		return nil
	})
}

func (s *SQLite) SaveRun(ctx context.Context, run Run, event *Event) error {
	return s.saveEntityWithEvent(ctx, func(exec executor) error {
		return saveRun(ctx, exec, run)
	}, event)
}

func saveRun(ctx context.Context, executor executor, run Run) error {
	configuration := run.Configuration
	if !json.Valid(configuration) {
		configuration = json.RawMessage(`{}`)
	}
	created := time.Now().UTC().UnixNano()
	_, err := executor.ExecContext(ctx, `INSERT INTO runs(id, name, state, started_at, ended_at, report, configuration_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, state=excluded.state, started_at=excluded.started_at, ended_at=excluded.ended_at, report=excluded.report, configuration_json=excluded.configuration_json`,
		run.ID, run.Name, run.State, run.StartedAt.UnixNano(), run.EndedAt.UnixNano(), run.Report, string(configuration), created)
	return err
}

func (s *SQLite) SaveTask(ctx context.Context, task scheduler.Task, event *Event) error {
	return s.saveEntityWithEvent(ctx, func(exec executor) error {
		return saveTask(ctx, exec, task)
	}, event)
}

func saveTask(ctx context.Context, executor executor, task scheduler.Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO tasks(run_id, id, state, idempotency_key, data_json, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, id) DO UPDATE SET state=excluded.state, idempotency_key=excluded.idempotency_key, data_json=excluded.data_json, updated_at=excluded.updated_at`,
		task.RunID, task.ID, string(task.State), task.IdempotencyKey, data, time.Now().UTC().UnixNano())
	return err
}

func (s *SQLite) CompleteTask(ctx context.Context, task scheduler.Task, finding Finding, message *messaging.Message, event Event) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		if err := saveTask(ctx, tx, task); err != nil {
			return err
		}
		if err := saveFinding(ctx, tx, finding); err != nil {
			return err
		}
		if message != nil {
			if err := saveMessage(ctx, tx, *message); err != nil {
				return err
			}
		}
		return saveEvent(ctx, tx, event)
	})
}

func (s *SQLite) SaveFinding(ctx context.Context, finding Finding, event *Event) error {
	return s.saveEntityWithEvent(ctx, func(exec executor) error {
		return saveFinding(ctx, exec, finding)
	}, event)
}

func saveFinding(ctx context.Context, executor executor, finding Finding) error {
	evidence, err := json.Marshal(finding.Evidence)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO findings(id, run_id, task_id, agent_id, role, kind, summary, evidence_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, finding.ID, finding.RunID, finding.TaskID, finding.AgentID, finding.Role, finding.Kind, finding.Summary, evidence, finding.CreatedAt.UnixNano())
	return err
}

func (s *SQLite) SaveMessage(ctx context.Context, message messaging.Message, event *Event) error {
	return s.saveEntityWithEvent(ctx, func(exec executor) error {
		return saveMessage(ctx, exec, message)
	}, event)
}

func saveMessage(ctx context.Context, executor executor, message messaging.Message) error {
	_, err := executor.ExecContext(ctx, `INSERT INTO messages(id, run_id, sender, recipient, task_id, timestamp, type, payload_json, correlation_id, idempotency_key) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, message.ID, message.RunID, message.Sender, message.Recipient, message.TaskID, message.Timestamp.UnixNano(), string(message.Type), message.Payload, message.CorrelationID, message.IdempotencyKey)
	return err
}

func (s *SQLite) SaveEvent(ctx context.Context, event Event) error {
	return s.transaction(ctx, func(tx *sql.Tx) error { return saveEvent(ctx, tx, event) })
}

func saveEvent(ctx context.Context, executor executor, event Event) error {
	if !json.Valid(event.Payload) {
		event.Payload = json.RawMessage(`{}`)
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO events(id, run_id, sequence, type, schema_version, timestamp, actor, task_id, payload_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, event.ID, event.RunID, event.Sequence, event.Type, event.SchemaVersion, event.Timestamp.UnixNano(), event.Actor, event.TaskID, event.Payload)
	return err
}

func (s *SQLite) SaveArtifact(ctx context.Context, record artifact.Record) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts(id, run_id, path, sha256, size_bytes, media_type, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`, record.ID, record.RunID, record.Path, record.SHA256, record.SizeBytes, record.MediaType, record.CreatedAt.UnixNano())
	return err
}

func (s *SQLite) SaveBudget(ctx context.Context, runID string, status budget.Status) error {
	return s.transaction(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) + 1 FROM budget_snapshots WHERE run_id = ?", runID).Scan(&next); err != nil {
			return err
		}
		data, err := json.Marshal(status)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO budget_snapshots(run_id, id, status_json, created_at) VALUES (?, ?, ?, ?)", runID, next, data, time.Now().UTC().UnixNano())
		return err
	})
}

func (s *SQLite) Load(ctx context.Context, runID string) (Snapshot, error) {
	var snapshot Snapshot
	var run Run
	var started, ended int64
	var configuration []byte
	err := s.db.QueryRowContext(ctx, "SELECT id, name, state, started_at, ended_at, report, configuration_json FROM runs WHERE id = ?", runID).Scan(&run.ID, &run.Name, &run.State, &started, &ended, &run.Report, &configuration)
	if err == nil {
		run.StartedAt = time.Unix(0, started).UTC()
		if ended > 0 {
			run.EndedAt = time.Unix(0, ended).UTC()
		}
		run.Configuration = configuration
		snapshot.Run = &run
	} else if !errors.Is(err, sql.ErrNoRows) {
		return snapshot, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT data_json FROM tasks WHERE run_id = ? ORDER BY id", runID)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return snapshot, err
		}
		var task scheduler.Task
		if err := json.Unmarshal(data, &task); err != nil {
			return snapshot, err
		}
		snapshot.Tasks = append(snapshot.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return snapshot, err
	}
	if err := s.loadEvents(ctx, runID, &snapshot); err != nil {
		return snapshot, err
	}
	if err := s.loadFindings(ctx, runID, &snapshot); err != nil {
		return snapshot, err
	}
	if err := s.loadMessages(ctx, runID, &snapshot); err != nil {
		return snapshot, err
	}
	if err := s.loadArtifacts(ctx, runID, &snapshot); err != nil {
		return snapshot, err
	}
	var data []byte
	err = s.db.QueryRowContext(ctx, "SELECT status_json FROM budget_snapshots WHERE run_id = ? ORDER BY id DESC LIMIT 1", runID).Scan(&data)
	if err == nil {
		var status budget.Status
		if err := json.Unmarshal(data, &status); err != nil {
			return snapshot, err
		}
		snapshot.Budget = &status
	} else if !errors.Is(err, sql.ErrNoRows) {
		return snapshot, err
	}
	return snapshot, nil
}

func (s *SQLite) loadEvents(ctx context.Context, runID string, snapshot *Snapshot) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, run_id, sequence, type, schema_version, timestamp, actor, task_id, payload_json FROM events WHERE run_id = ? ORDER BY sequence", runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var event Event
		var timestamp int64
		if err := rows.Scan(&event.ID, &event.RunID, &event.Sequence, &event.Type, &event.SchemaVersion, &timestamp, &event.Actor, &event.TaskID, &event.Payload); err != nil {
			return err
		}
		event.Timestamp = time.Unix(0, timestamp).UTC()
		snapshot.Events = append(snapshot.Events, event)
	}
	return rows.Err()
}

func (s *SQLite) loadFindings(ctx context.Context, runID string, snapshot *Snapshot) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, run_id, task_id, agent_id, role, kind, summary, evidence_json, created_at FROM findings WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var finding Finding
		var created int64
		var evidence []byte
		if err := rows.Scan(&finding.ID, &finding.RunID, &finding.TaskID, &finding.AgentID, &finding.Role, &finding.Kind, &finding.Summary, &evidence, &created); err != nil {
			return err
		}
		finding.CreatedAt = time.Unix(0, created).UTC()
		if err := json.Unmarshal(evidence, &finding.Evidence); err != nil {
			return err
		}
		snapshot.Findings = append(snapshot.Findings, finding)
	}
	return rows.Err()
}

func (s *SQLite) loadMessages(ctx context.Context, runID string, snapshot *Snapshot) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, run_id, sender, recipient, task_id, timestamp, type, payload_json, correlation_id, idempotency_key FROM messages WHERE run_id = ? ORDER BY timestamp, id", runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var message messaging.Message
		var timestamp int64
		var messageType string
		if err := rows.Scan(&message.ID, &message.RunID, &message.Sender, &message.Recipient, &message.TaskID, &timestamp, &messageType, &message.Payload, &message.CorrelationID, &message.IdempotencyKey); err != nil {
			return err
		}
		message.Timestamp = time.Unix(0, timestamp).UTC()
		message.Type = messaging.Type(messageType)
		snapshot.Messages = append(snapshot.Messages, message)
	}
	return rows.Err()
}

func (s *SQLite) loadArtifacts(ctx context.Context, runID string, snapshot *Snapshot) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id, run_id, path, sha256, size_bytes, media_type, created_at FROM artifacts WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var record artifact.Record
		var created int64
		if err := rows.Scan(&record.ID, &record.RunID, &record.Path, &record.SHA256, &record.SizeBytes, &record.MediaType, &created); err != nil {
			return err
		}
		record.CreatedAt = time.Unix(0, created).UTC()
		snapshot.Artifacts = append(snapshot.Artifacts, record)
	}
	return rows.Err()
}

func (s *SQLite) Checkpoint(ctx context.Context) error {
	var busy, log, checkpointed int
	return s.db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed)
}

func (s *SQLite) transaction(ctx context.Context, operation func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}
