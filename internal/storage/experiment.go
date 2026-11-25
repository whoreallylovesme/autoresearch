package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/experiment"
)

func (s *SQLite) SaveExperiment(ctx context.Context, record experiment.Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO experiments(run_id, id, hypothesis_id, status, executor_name, data_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, id) DO UPDATE SET status=excluded.status, data_json=excluded.data_json, updated_at=excluded.updated_at`, record.RunID, record.ID, record.HypothesisID, record.Status, record.ExecutorName, data, record.CreatedAt.UnixNano(), time.Now().UTC().UnixNano())
	return err
}

func (s *SQLite) LoadExperiment(ctx context.Context, runID, id string) (experiment.Record, error) {
	var record experiment.Record
	var data []byte
	err := s.db.QueryRowContext(ctx, "SELECT data_json FROM experiments WHERE run_id = ? AND id = ?", runID, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return record, errors.New("experiment not found")
	}
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *SQLite) LoadExperiments(ctx context.Context, runID string) ([]experiment.Record, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT data_json FROM experiments WHERE run_id = ? ORDER BY created_at, id", runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []experiment.Record
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var record experiment.Record
		if err := json.Unmarshal(data, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
