package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

var ErrAlreadyExists = errors.New("record already exists")

type Run struct {
	ID            string
	Name          string
	State         string
	StartedAt     time.Time
	EndedAt       time.Time
	Report        string
	Configuration json.RawMessage
}

type Event struct {
	ID            string
	RunID         string
	Sequence      uint64
	Type          string
	SchemaVersion int
	Timestamp     time.Time
	Actor         string
	TaskID        string
	Payload       json.RawMessage
}

type Finding struct {
	ID        string
	RunID     string
	TaskID    string
	AgentID   string
	Role      string
	Kind      string
	Summary   string
	Evidence  []string
	CreatedAt time.Time
}

type Snapshot struct {
	Run       *Run
	Tasks     []scheduler.Task
	Events    []Event
	Findings  []Finding
	Messages  []messaging.Message
	Artifacts []artifact.Record
	Budget    *budget.Status
}

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}
