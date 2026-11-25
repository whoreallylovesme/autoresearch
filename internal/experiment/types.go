package experiment

import (
	"encoding/json"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
	"github.com/whoreallylovesme/autoresearch/internal/verifier"
)

type Status string

const (
	StatusCreated      Status = "created"
	StatusRunning      Status = "running"
	StatusCompleted    Status = "completed"
	StatusFailed       Status = "failed"
	StatusCancelled    Status = "cancelled"
	StatusVerified     Status = "verified"
	StatusRefuted      Status = "refuted"
	StatusInconclusive Status = "inconclusive"
)

type Record struct {
	ID             string                 `json:"id"`
	RunID          string                 `json:"run_id"`
	HypothesisID   string                 `json:"hypothesis_id"`
	Status         Status                 `json:"status"`
	CodeSHA256     string                 `json:"code_sha256,omitempty"`
	GitCommit      string                 `json:"git_commit,omitempty"`
	Configuration  json.RawMessage        `json:"configuration"`
	DatasetVersion string                 `json:"dataset_version,omitempty"`
	Environment    map[string]string      `json:"environment,omitempty"`
	Dependencies   []string               `json:"dependencies,omitempty"`
	RandomSeed     int64                  `json:"random_seed"`
	Hardware       map[string]string      `json:"hardware,omitempty"`
	ExecutorName   string                 `json:"executor_name"`
	Job            executor.Job           `json:"job"`
	Metrics        map[string]float64     `json:"metrics,omitempty"`
	Logs           []executor.LogEntry    `json:"logs,omitempty"`
	Artifacts      []artifact.Record      `json:"artifacts,omitempty"`
	Verification   *verifier.Verification `json:"verification,omitempty"`
	Error          string                 `json:"error,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
	StartedAt      time.Time              `json:"started_at,omitempty"`
	EndedAt        time.Time              `json:"ended_at,omitempty"`
}
