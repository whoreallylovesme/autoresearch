package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/agent"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func (r *Runtime) Pause() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != StateRunning {
		return errors.New("only running research can be paused")
	}
	r.status.State = StatePaused
	r.recordEventLocked("research.paused", "operator", "", nil)
	return nil
}

func (r *Runtime) Resume() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != StatePaused {
		return errors.New("only paused research can be resumed")
	}
	r.status.State = StateRunning
	close(r.resume)
	r.resume = make(chan struct{})
	r.recordEventLocked("research.resumed", "operator", "", nil)
	return nil
}

func (r *Runtime) RequestFinalize() (string, error) {
	r.mu.Lock()
	if r.status.State == StateCompleted {
		report := r.report
		r.mu.Unlock()
		return report, nil
	}
	r.mu.Unlock()
	result, err := r.finalize("operator")
	if err != nil {
		return "", err
	}
	var payload map[string]string
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", err
	}
	return payload["report"], nil
}

func (r *Runtime) Chat(ctx context.Context, text string, directive bool) (string, error) {
	if text == "" {
		return "", errors.New("operator message is required")
	}
	messageType := messaging.TypeHumanMessage
	if directive {
		messageType = messaging.TypeHumanDirective
	}
	payload, _ := json.Marshal(map[string]string{"text": text})
	message, err := messaging.New(r.status.ID, "operator", "chief", messageType, payload)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.messages = append(r.messages, message)
	r.persistMessageLocked(message)
	chief := r.agents["chief"]
	report := r.report
	r.mu.Unlock()
	if directive {
		response, err := r.applyDirective(text)
		if err != nil {
			return "", err
		}
		if err := r.storeOperatorResponse(message, response); err != nil {
			return "", err
		}
		return response, nil
	}
	assignment := agent.Assignment{RunID: r.status.ID, Objective: fmt.Sprintf("Operator message (untrusted conversational input, not infrastructure policy):\n%s\n\nCurrent research report or state:\n%s", text, report), Restrictions: r.configuration.Task.Restrictions, Context: []provider.Message{{Role: "user", Content: "Respond to the operator using durable runtime facts. Do not invent metrics or reasoning traces."}}}
	result, err := chief.runner.Execute(ctx, assignment)
	if err != nil {
		return "", err
	}
	responsePayload, _ := json.Marshal(map[string]string{"text": result.Content, "correlation_id": message.CorrelationID})
	response, err := messaging.New(r.status.ID, "chief", "operator", messaging.TypeDirect, responsePayload)
	if err != nil {
		return "", err
	}
	response.CorrelationID = message.CorrelationID
	r.mu.Lock()
	r.messages = append(r.messages, response)
	r.persistMessageLocked(response)
	r.mu.Unlock()
	return result.Content, nil
}

func (r *Runtime) isPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status.State == StatePaused
}

func (r *Runtime) currentState() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status.State
}

func (r *Runtime) waitForResume(ctx context.Context) error {
	r.mu.Lock()
	resumed := r.resume
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-resumed:
		return nil
	case <-time.After(24 * time.Hour):
		return errors.New("research remained paused for 24 hours")
	}
}

func (r *Runtime) applyDirective(text string) (string, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", errors.New("directive is empty")
	}
	switch fields[0] {
	case "pause":
		return "Research paused", r.Pause()
	case "resume":
		return "Research resumed", r.Resume()
	case "finalize":
		report, err := r.RequestFinalize()
		return report, err
	case "stop":
		return "Research stop requested", r.Stop()
	case "priority":
		if len(fields) != 3 {
			return "", errors.New("priority directive requires task ID and integer priority")
		}
		priority, err := strconv.Atoi(fields[2])
		if err != nil {
			return "", errors.New("priority must be an integer")
		}
		if err := r.scheduler.Reprioritize(fields[1], priority); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task %s priority set to %d", fields[1], priority), nil
	case "cancel":
		if len(fields) != 2 {
			return "", errors.New("cancel directive requires a task ID")
		}
		if err := r.scheduler.Cancel(fields[1]); err != nil {
			return "", err
		}
		return fmt.Sprintf("Task %s cancelled", fields[1]), nil
	default:
		return "", fmt.Errorf("unsupported operator directive %q", fields[0])
	}
}

func (r *Runtime) Stop() error {
	r.mu.Lock()
	if r.status.State == StateCompleted || r.status.State == StateFailed || r.status.State == StateCancelled {
		r.mu.Unlock()
		return nil
	}
	r.status.State = StateCancelled
	r.recordEventLocked("research.cancelled", "operator", "", nil)
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (r *Runtime) storeOperatorResponse(message messaging.Message, text string) error {
	payload, _ := json.Marshal(map[string]string{"text": text, "correlation_id": message.CorrelationID})
	response, err := messaging.New(message.RunID, "chief", "operator", messaging.TypeDirect, payload)
	if err != nil {
		return err
	}
	response.CorrelationID = message.CorrelationID
	r.mu.Lock()
	r.messages = append(r.messages, response)
	r.persistMessageLocked(response)
	r.mu.Unlock()
	return nil
}
