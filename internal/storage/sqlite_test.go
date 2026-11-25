package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestSQLiteRoundTripPersistsTransactionalResearchState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "research.db")
	store, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := storage.Run{ID: "run-1", Name: "demo", State: "running", StartedAt: time.Now().UTC(), Configuration: json.RawMessage(`{"version":1}`)}
	if err := store.SaveRun(ctx, run, nil); err != nil {
		t.Fatal(err)
	}
	var journalMode string
	if err := store.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %s", journalMode)
	}
	task := scheduler.Task{ID: "task-1", RunID: run.ID, Role: "worker", State: scheduler.StateRunning, IdempotencyKey: "operation-1", CreatedAt: time.Now().UTC()}
	created := storage.Event{ID: "event-1", RunID: run.ID, Sequence: 1, Type: "task.created", SchemaVersion: 1, Timestamp: time.Now().UTC(), Actor: "chief", TaskID: task.ID, Payload: json.RawMessage(`{}`)}
	if err := store.SaveTask(ctx, task, &created); err != nil {
		t.Fatal(err)
	}
	task.State = scheduler.StateCompleted
	task.Result.Output = "verified output"
	finding := storage.Finding{ID: "finding-1", RunID: run.ID, TaskID: task.ID, AgentID: "worker-1", Role: "worker", Kind: "finding", Summary: "verified output", CreatedAt: time.Now().UTC()}
	message, err := messaging.New(run.ID, "worker-1", "chief", messaging.TypeFinding, json.RawMessage(`{"summary":"verified output"}`))
	if err != nil {
		t.Fatal(err)
	}
	message.TaskID = task.ID
	completed := storage.Event{ID: "event-2", RunID: run.ID, Sequence: 2, Type: "task.completed", SchemaVersion: 1, Timestamp: time.Now().UTC(), Actor: "worker-1", TaskID: task.ID, Payload: json.RawMessage(`{}`)}
	if err := store.CompleteTask(ctx, task, finding, &message, completed); err != nil {
		t.Fatal(err)
	}
	status := budget.Status{Confirmed: budget.Usage{CostMicros: 42, Requests: 1}}
	if err := store.SaveBudget(ctx, run.ID, status); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveArtifact(ctx, artifact.Record{ID: "artifact-1", RunID: run.ID, Path: "report.md", SHA256: "abc123", SizeBytes: 10, MediaType: "text/markdown", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.Checkpoint(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Load(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run == nil || snapshot.Run.State != "running" || len(snapshot.Tasks) != 1 || snapshot.Tasks[0].Result.Output != "verified output" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if len(snapshot.Events) != 2 || len(snapshot.Findings) != 1 || len(snapshot.Messages) != 1 {
		t.Fatalf("snapshot counts = %#v", snapshot)
	}
	if len(snapshot.Artifacts) != 1 || snapshot.Artifacts[0].SHA256 != "abc123" {
		t.Fatalf("artifacts = %#v", snapshot.Artifacts)
	}
	if snapshot.Budget == nil || snapshot.Budget.Confirmed.CostMicros != 42 {
		t.Fatalf("budget = %#v", snapshot.Budget)
	}
}

func TestSQLiteRejectsDuplicateEventSequence(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveRun(ctx, storage.Run{ID: "run", Name: "run", State: "created"}, nil); err != nil {
		t.Fatal(err)
	}
	first := storage.Event{ID: "one", RunID: "run", Sequence: 1, Type: "research.started", SchemaVersion: 1, Timestamp: time.Now().UTC(), Actor: "runtime", Payload: json.RawMessage(`{}`)}
	if err := store.SaveEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	duplicate := first
	duplicate.ID = "two"
	if err := store.SaveEvent(ctx, duplicate); err == nil {
		t.Fatal("duplicate per-run sequence was accepted")
	}
	if err := store.SaveEvent(ctx, first); err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("idempotent event retry error = %v", err)
	}
}

func TestSQLiteMessageInsertIsIdempotent(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveRun(ctx, storage.Run{ID: "run", Name: "run", State: "created"}, nil); err != nil {
		t.Fatal(err)
	}
	message, err := messaging.New("run", "chief", "worker", messaging.TypeDirect, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(ctx, message, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(ctx, message, nil); err != nil {
		t.Fatalf("duplicate message error = %v", err)
	}
	snapshot, err := store.Load(ctx, "run")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Messages) != 1 {
		t.Fatalf("messages = %d", len(snapshot.Messages))
	}
}
