package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/agent"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func TestAgentRejectsUnauthorizedToolAndRecovers(t *testing.T) {
	mock := provider.NewMock(
		provider.Response{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "delete_everything", Arguments: json.RawMessage(`{}`)}}},
		provider.Response{Content: "finished safely"},
	)
	dispatcher := &recordingDispatcher{}
	runner := newRunner(mock, dispatcher, []string{"submit_finding"}, 3)
	result, err := runner.Execute(context.Background(), agent.Assignment{TaskID: "task", Objective: "research"})
	if err != nil || result.Content != "finished safely" {
		t.Fatalf("Execute() = %#v, %v", result, err)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("unauthorized tool reached dispatcher %d times", dispatcher.calls)
	}
	requests := mock.Requests()
	if len(requests) != 2 || !strings.Contains(requests[1].Messages[len(requests[1].Messages)-1].Content, "not permitted") {
		t.Fatalf("tool rejection missing from next request: %#v", requests)
	}
}

func TestAgentReturnsToolResultToModel(t *testing.T) {
	mock := provider.NewMock(
		provider.Response{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "submit_finding", Arguments: json.RawMessage(`{"claim":"supported"}`)}}},
		provider.Response{Content: "done"},
	)
	dispatcher := &recordingDispatcher{result: json.RawMessage(`{"accepted":true}`)}
	runner := newRunner(mock, dispatcher, []string{"submit_finding"}, 3)
	_, err := runner.Execute(context.Background(), agent.Assignment{Objective: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher.calls != 1 || string(dispatcher.arguments) != `{"claim":"supported"}` {
		t.Fatalf("dispatcher = %#v", dispatcher)
	}
	messages := mock.Requests()[1].Messages
	if messages[len(messages)-1].Role != "tool" || !strings.Contains(messages[len(messages)-1].Content, "accepted") {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestAgentRejectsMalformedToolArguments(t *testing.T) {
	mock := provider.NewMock(
		provider.Response{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "submit_finding", Arguments: json.RawMessage(`not-json`)}}},
		provider.Response{Content: "recovered"},
	)
	dispatcher := &recordingDispatcher{}
	runner := newRunner(mock, dispatcher, []string{"submit_finding"}, 3)
	_, err := runner.Execute(context.Background(), agent.Assignment{})
	if err != nil || dispatcher.calls != 0 {
		t.Fatalf("Execute() error = %v, calls = %d", err, dispatcher.calls)
	}
}

func TestAgentStopsAtIterationLimit(t *testing.T) {
	mock := provider.NewMockHandler(func(_ context.Context, _ provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{{ID: "call", Name: "submit_finding", Arguments: json.RawMessage(`{}`)}}}, nil
	})
	runner := newRunner(mock, &recordingDispatcher{}, []string{"submit_finding"}, 2)
	_, err := runner.Execute(context.Background(), agent.Assignment{})
	if !errors.Is(err, agent.ErrIterationLimit) {
		t.Fatalf("error = %v", err)
	}
}

func TestAgentPropagatesCancellation(t *testing.T) {
	mock := provider.NewMockHandler(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	})
	runner := newRunner(mock, &recordingDispatcher{}, nil, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := runner.Execute(ctx, agent.Assignment{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

type recordingDispatcher struct {
	mu        sync.Mutex
	calls     int
	arguments json.RawMessage
	result    json.RawMessage
}

func (d *recordingDispatcher) Definitions(string) []provider.ToolDefinition {
	return []provider.ToolDefinition{{Name: "submit_finding", Description: "Submit", Parameters: json.RawMessage(`{"type":"object"}`)}}
}

func (d *recordingDispatcher) Execute(_ context.Context, _ string, _ string, arguments json.RawMessage) (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.arguments = append(json.RawMessage(nil), arguments...)
	return d.result, nil
}

func newRunner(p provider.Provider, dispatcher agent.ToolDispatcher, tools []string, iterations int) *agent.Agent {
	return agent.New("agent-1", config.AgentConfig{Role: "baby-scientist", Prompt: config.PromptConfig{Content: "system"}, Tools: tools, Model: config.ModelConfig{Parameters: map[string]any{"max_output_tokens": 128}}}, p, dispatcher, iterations)
}
