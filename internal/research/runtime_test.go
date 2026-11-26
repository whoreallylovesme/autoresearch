package research_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

func TestChiefDelegatesToWorkersAndAggregatesFindings(t *testing.T) {
	cfg := loadConfig(t)
	runtime, err := research.New(cfg, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	status := runtime.Status()
	if status.State != research.StateCompleted {
		t.Fatalf("state = %s", status.State)
	}
	tasks := runtime.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("tasks = %#v", tasks)
	}
	for _, task := range tasks {
		if task.State != scheduler.StateCompleted {
			t.Fatalf("task = %#v", task)
		}
	}
	findings := runtime.Findings()
	if len(findings) < 3 {
		t.Fatalf("findings = %#v", findings)
	}
	var criticism bool
	for _, finding := range findings {
		if finding.Kind == "criticism" {
			criticism = true
		}
	}
	if !criticism {
		t.Fatal("independent criticism is missing")
	}
	if !strings.Contains(runtime.Report(), "research-a") || !strings.Contains(runtime.Report(), "Criticism") {
		t.Fatalf("report = %q", runtime.Report())
	}
	if status.Budget.Confirmed.Requests < 6 || status.Budget.Reserved.Requests != 0 {
		t.Fatalf("budget = %#v", status.Budget)
	}
	events := runtime.Events()
	for index, event := range events {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event sequence at %d = %d", index, event.Sequence)
		}
	}
}

func TestRuntimeCancellationStopsChiefAndWorkers(t *testing.T) {
	cfg := loadConfig(t)
	factory := func(_ string, agentConfig config.AgentConfig) (provider.Provider, error) {
		return provider.NewMockHandler(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
			if agentConfig.Role == "chief-scientist" {
				<-ctx.Done()
				return provider.Response{}, ctx.Err()
			}
			return provider.Response{Content: "unused"}, nil
		}), nil
	}
	runtime, err := research.New(cfg, factory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := runtime.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
	if runtime.Status().State != research.StateCancelled {
		t.Fatalf("state = %s", runtime.Status().State)
	}
}

func TestRuntimeRejectsRealProviderWithoutPricing(t *testing.T) {
	cfg := loadConfig(t)
	cfg.Chief.Model.Provider = "openai"
	cfg.Chief.Model.APIKeyEnv = "TEST_UNUSED_KEY"
	_, err := research.New(cfg, func(string, config.AgentConfig) (provider.Provider, error) {
		return provider.NewMock(), nil
	})
	if err == nil || !strings.Contains(err.Error(), "pricing") {
		t.Fatalf("error = %v", err)
	}
}

func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../configs/autoresearch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestPauseAndResumeResearch(t *testing.T) {
	t.Setenv("AUTORESEARCH_DEMO_DELAY", "30ms")
	cfg := loadConfig(t)
	runtime, err := research.New(cfg, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- runtime.Run(context.Background()) }()
	waitForRuntimeState(t, runtime, research.StateRunning)
	if err := runtime.Pause(); err != nil {
		t.Fatal(err)
	}
	if runtime.Status().State != research.StatePaused {
		t.Fatalf("state = %s", runtime.Status().State)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resumed run did not complete")
	}
}

func TestPersistentOperatorReplies(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		directive   bool
		requestType messaging.Type
	}{
		{name: "ordinary chat", text: "Explain current research state", requestType: messaging.TypeHumanMessage},
		{name: "directive", text: "stop", directive: true, requestType: messaging.TypeHumanDirective},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := loadConfig(t)
			configuration.Runtime.DataDir = t.TempDir()
			runtime, err := research.NewPersistent(configuration, research.DemoFactory())
			if err != nil {
				t.Fatal(err)
			}
			response, err := runtime.Chat(context.Background(), test.text, test.directive)
			if err != nil || response == "" {
				t.Fatalf("Chat() = %q, %v", response, err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			runtime, err = research.NewPersistent(configuration, research.DemoFactory())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()

			messages := runtime.Messages()
			if len(messages) != 2 {
				t.Fatalf("messages = %#v", messages)
			}
			request, reply := messages[0], messages[1]
			if request.Type != test.requestType {
				t.Fatalf("request type = %q, want %q", request.Type, test.requestType)
			}
			if reply.Type != messaging.TypeDirect || reply.Sender != "chief" || reply.Recipient != "operator" {
				t.Fatalf("reply = %#v", reply)
			}
			if reply.RunID != request.RunID {
				t.Fatalf("reply run ID = %q, request run ID = %q", reply.RunID, request.RunID)
			}
			if reply.CorrelationID != request.CorrelationID {
				t.Fatalf("reply correlation ID = %q, request correlation ID = %q", reply.CorrelationID, request.CorrelationID)
			}
			var payload struct {
				Text          string `json:"text"`
				CorrelationID string `json:"correlation_id"`
			}
			if err := json.Unmarshal(reply.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Text != response {
				t.Fatalf("reply text = %q, Chat response = %q", payload.Text, response)
			}
			if payload.CorrelationID != request.CorrelationID {
				t.Fatalf("payload correlation ID = %q, request correlation ID = %q", payload.CorrelationID, request.CorrelationID)
			}
		})
	}
}

func TestPersistentOperatorChatProviderFailureDoesNotAppendReply(t *testing.T) {
	providerErr := errors.New("provider failed")
	configuration := loadConfig(t)
	configuration.Runtime.DataDir = t.TempDir()
	runtime, err := research.NewPersistent(configuration, func(_ string, _ config.AgentConfig) (provider.Provider, error) {
		return provider.NewMockHandler(func(context.Context, provider.Request) (provider.Response, error) {
			return provider.Response{}, providerErr
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := runtime.Chat(context.Background(), "Explain current research state", false)
	if response != "" || !errors.Is(err, providerErr) {
		t.Fatalf("Chat() = %q, %v", response, err)
	}
	if messages := runtime.Messages(); len(messages) != 1 || messages[0].Type != messaging.TypeHumanMessage {
		t.Fatalf("messages after provider failure = %#v", messages)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = research.NewPersistent(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if messages := runtime.Messages(); len(messages) != 1 || messages[0].Type != messaging.TypeHumanMessage {
		t.Fatalf("persisted messages after provider failure = %#v", messages)
	}
}

func waitForRuntimeState(t *testing.T, runtime *research.Runtime, expected research.State) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if runtime.Status().State == expected {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state did not become %s", expected)
}
