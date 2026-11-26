package messaging

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/id"
)

type Type string

const (
	TypeDirect         Type = "direct"
	TypeDelegation     Type = "task.delegation"
	TypeResult         Type = "task.result"
	TypeClarification  Type = "clarification"
	TypeBroadcast      Type = "broadcast"
	TypeFinding        Type = "finding"
	TypeCriticism      Type = "criticism"
	TypeExperiment     Type = "experiment.request"
	TypeHumanMessage   Type = "human.message"
	TypeHumanDirective Type = "human.directive"
)

type Message struct {
	ID             string          `json:"id"`
	RunID          string          `json:"run_id"`
	Sender         string          `json:"sender"`
	Recipient      string          `json:"recipient"`
	TaskID         string          `json:"task_id,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
	Type           Type            `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	CorrelationID  string          `json:"correlation_id"`
	IdempotencyKey string          `json:"idempotency_key"`
}

func New(runID, sender, recipient string, messageType Type, payload json.RawMessage) (Message, error) {
	messageID, err := id.New()
	if err != nil {
		return Message{}, err
	}
	correlationID, err := id.New()
	if err != nil {
		return Message{}, err
	}
	message := Message{
		ID: messageID, RunID: runID, Sender: sender, Recipient: recipient, Timestamp: time.Now().UTC(),
		Type: messageType, Payload: payload, CorrelationID: correlationID, IdempotencyKey: messageID,
	}
	if err := message.Validate(); err != nil {
		return Message{}, err
	}
	return message, nil
}

func (m Message) Validate() error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.RunID) == "" || strings.TrimSpace(m.Sender) == "" || strings.TrimSpace(m.Recipient) == "" || m.Timestamp.IsZero() || m.Type == "" || strings.TrimSpace(m.CorrelationID) == "" || strings.TrimSpace(m.IdempotencyKey) == "" {
		return errors.New("message identity, participants, timestamp, type, correlation, and idempotency key are required")
	}
	if !json.Valid(m.Payload) {
		return errors.New("message payload must be valid JSON")
	}
	return nil
}
