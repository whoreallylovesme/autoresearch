package research

import (
	"encoding/json"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/agent"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

type State string

const (
	StateCreated    State = "created"
	StateRunning    State = "running"
	StateFinalizing State = "finalizing"
	StateCompleted  State = "completed"
	StateFailed     State = "failed"
	StateCancelled  State = "cancelled"
)

type Status struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	State     State         `json:"state"`
	StartedAt time.Time     `json:"started_at"`
	EndedAt   time.Time     `json:"ended_at,omitempty"`
	Budget    budget.Status `json:"budget"`
}

type Finding struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	AgentID   string    `json:"agent_id"`
	Role      string    `json:"role"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	Evidence  []string  `json:"evidence,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentStatus struct {
	ID                 string         `json:"id"`
	Role               string         `json:"role"`
	Model              string         `json:"model"`
	Provider           string         `json:"provider"`
	State              string         `json:"state"`
	CurrentTask        string         `json:"current_task,omitempty"`
	LastHeartbeat      time.Time      `json:"last_heartbeat"`
	LastActivity       time.Time      `json:"last_activity"`
	LastMeaningfulWork time.Time      `json:"last_meaningful_work"`
	Usage              provider.Usage `json:"usage"`
	LastError          string         `json:"last_error,omitempty"`
}

type Event struct {
	ID            string          `json:"id"`
	RunID         string          `json:"run_id"`
	SchemaVersion int             `json:"schema_version"`
	Sequence      uint64          `json:"sequence"`
	Type          string          `json:"type"`
	Timestamp     time.Time       `json:"timestamp"`
	Actor         string          `json:"actor"`
	TaskID        string          `json:"task_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type agentAssignment = agent.Assignment
type agentResult = agent.Result
