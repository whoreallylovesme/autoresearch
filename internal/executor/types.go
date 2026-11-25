package executor

import (
	"context"
	"errors"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
)

var ErrJobNotFound = errors.New("executor job not found")

type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

type Job struct {
	ID        string            `json:"id"`
	RunID     string            `json:"run_id"`
	Workspace string            `json:"workspace"`
	Image     string            `json:"image"`
	Command   []string          `json:"command"`
	Env       map[string]string `json:"env,omitempty"`
	Timeout   time.Duration     `json:"timeout"`
	MemoryMB  int               `json:"memory_mb"`
	CPUs      float64           `json:"cpus"`
	PIDs      int               `json:"pids"`
	Network   string            `json:"network"`
}

type Result struct {
	ID        string    `json:"id"`
	State     State     `json:"state"`
	ExitCode  int       `json:"exit_code"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Error     string    `json:"error,omitempty"`
}

type LogEntry struct {
	Sequence  uint64    `json:"sequence"`
	Stream    string    `json:"stream"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

type Executor interface {
	Submit(context.Context, Job) error
	Status(context.Context, string) (Result, error)
	Cancel(context.Context, string) error
	Wait(context.Context, string) (Result, error)
	Logs(context.Context, string) (<-chan LogEntry, error)
	Collect(context.Context, string) ([]artifact.Record, error)
}
