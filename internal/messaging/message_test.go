package messaging_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/messaging"
)

func TestMessageValidation(t *testing.T) {
	message := messaging.Message{
		ID: "message-1", RunID: "run-1", Sender: "chief", Recipient: "worker", TaskID: "task-1",
		Timestamp: time.Now().UTC(), Type: messaging.TypeDelegation, Payload: json.RawMessage(`{"title":"research"}`),
		CorrelationID: "correlation-1", IdempotencyKey: "operation-1",
	}
	if err := message.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	message.Payload = json.RawMessage(`not-json`)
	if err := message.Validate(); err == nil {
		t.Fatal("invalid payload was accepted")
	}
}

func TestNewMessageAddsIdentityAndTimestamp(t *testing.T) {
	message, err := messaging.New("run-1", "chief", "worker", messaging.TypeFinding, json.RawMessage(`{"claim":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	if message.ID == "" || message.Timestamp.IsZero() || message.CorrelationID == "" {
		t.Fatalf("message = %#v", message)
	}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
}
