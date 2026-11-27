package storage_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestSQLiteSaveEntityWithOptionalEvent(t *testing.T) {
	ctx := context.Background()
	timestamp := time.Unix(0, 1).UTC()
	run := storage.Run{ID: "run-1", Name: "research", State: "running", StartedAt: timestamp, Configuration: json.RawMessage(`{}`)}
	task := scheduler.Task{ID: "task-1", RunID: run.ID, Role: "worker", State: scheduler.StateRunning, CreatedAt: timestamp}
	finding := storage.Finding{ID: "finding-1", RunID: run.ID, TaskID: task.ID, AgentID: "worker-1", Role: "worker", Kind: "finding", Summary: "result", CreatedAt: timestamp}
	message := messaging.Message{ID: "message-1", RunID: run.ID, Sender: "worker-1", Recipient: "chief", TaskID: task.ID, Timestamp: timestamp, Type: messaging.TypeFinding, Payload: json.RawMessage(`{}`), CorrelationID: "correlation-1", IdempotencyKey: "operation-1"}

	tests := []struct {
		name           string
		requiresParent bool
		entityCountSQL string
		save           func(context.Context, *storage.SQLite, *storage.Event) error
	}{
		{
			name:           "run",
			entityCountSQL: "SELECT COUNT(*) FROM runs WHERE id = 'run-1'",
			save: func(ctx context.Context, store *storage.SQLite, event *storage.Event) error {
				return store.SaveRun(ctx, run, event)
			},
		},
		{
			name:           "task",
			requiresParent: true,
			entityCountSQL: "SELECT COUNT(*) FROM tasks WHERE run_id = 'run-1' AND id = 'task-1'",
			save: func(ctx context.Context, store *storage.SQLite, event *storage.Event) error {
				return store.SaveTask(ctx, task, event)
			},
		},
		{
			name:           "finding",
			requiresParent: true,
			entityCountSQL: "SELECT COUNT(*) FROM findings WHERE id = 'finding-1'",
			save: func(ctx context.Context, store *storage.SQLite, event *storage.Event) error {
				return store.SaveFinding(ctx, finding, event)
			},
		},
		{
			name:           "message",
			requiresParent: true,
			entityCountSQL: "SELECT COUNT(*) FROM messages WHERE id = 'message-1'",
			save: func(ctx context.Context, store *storage.SQLite, event *storage.Event) error {
				return store.SaveMessage(ctx, message, event)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prepare := func(t *testing.T, store *storage.SQLite) {
				t.Helper()
				if test.requiresParent {
					if err := store.SaveRun(ctx, run, nil); err != nil {
						t.Fatal(err)
					}
				}
			}

			t.Run("nil event", func(t *testing.T) {
				store := openSQLiteSaveTestStore(t)
				prepare(t, store)
				if err := test.save(ctx, store, nil); err != nil {
					t.Fatal(err)
				}
				assertSQLiteSaveTestCount(t, store, test.entityCountSQL, 1)
				assertSQLiteSaveTestCount(t, store, "SELECT COUNT(*) FROM events", 0)
			})

			t.Run("non-nil event", func(t *testing.T) {
				store := openSQLiteSaveTestStore(t)
				prepare(t, store)
				event := storage.Event{ID: "event-1", RunID: run.ID, Sequence: 1, Type: "entity.saved", SchemaVersion: 1, Timestamp: timestamp, Actor: "runtime", TaskID: task.ID, Payload: json.RawMessage(`{}`)}
				if err := test.save(ctx, store, &event); err != nil {
					t.Fatal(err)
				}
				assertSQLiteSaveTestCount(t, store, test.entityCountSQL, 1)
				assertSQLiteSaveTestCount(t, store, "SELECT COUNT(*) FROM events WHERE id = 'event-1'", 1)
			})

			t.Run("event failure rolls back entity", func(t *testing.T) {
				store := openSQLiteSaveTestStore(t)
				prepare(t, store)
				event := storage.Event{ID: "event-1", RunID: "missing-run", Sequence: 1, Type: "entity.saved", SchemaVersion: 1, Timestamp: timestamp, Actor: "runtime", TaskID: task.ID, Payload: json.RawMessage(`{}`)}
				if err := test.save(ctx, store, &event); err == nil {
					t.Fatal("save with an invalid event succeeded")
				}
				assertSQLiteSaveTestCount(t, store, test.entityCountSQL, 0)
				assertSQLiteSaveTestCount(t, store, "SELECT COUNT(*) FROM events", 0)
			})
		})
	}
}

func openSQLiteSaveTestStore(t *testing.T) *storage.SQLite {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close SQLite store: %v", err)
		}
	})
	return store
}

func assertSQLiteSaveTestCount(t *testing.T, store *storage.SQLite, query string, want int) {
	t.Helper()
	var got int
	if err := store.DB().QueryRow(query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s: count = %d, want %d", query, got, want)
	}
}
