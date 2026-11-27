package research

import (
	"context"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

func TestOperatorDirectiveReprioritizesAndCancelsTasks(t *testing.T) {
	runtime, err := New(loadConfigForDirective(t), DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	runtime.status.State = StateRunning
	for _, task := range []scheduler.Task{{ID: "a", Role: "baby-scientist", Priority: 1}, {ID: "b", Role: "baby-scientist", Priority: 2}} {
		if err := runtime.scheduler.Submit(task); err != nil {
			t.Fatal(err)
		}
	}
	response, err := runtime.Chat(context.Background(), "priority a 10", true)
	if err != nil || response == "" {
		t.Fatalf("priority directive = %q, %v", response, err)
	}
	lease, err := runtime.scheduler.Lease(context.Background(), scheduler.Worker{ID: "test", Roles: []string{"baby-scientist"}})
	if err != nil || lease.Task.ID != "a" {
		t.Fatalf("lease = %#v, %v", lease, err)
	}
	response, err = runtime.Chat(context.Background(), "cancel b", true)
	if err != nil || response == "" {
		t.Fatalf("cancel directive = %q, %v", response, err)
	}
	task, _ := runtime.scheduler.Get("b")
	if task.State != scheduler.StateCancelled {
		t.Fatalf("task = %#v", task)
	}
}

func TestOrdinaryOperatorChatResponseKeepsRequestRunID(t *testing.T) {
	var runtime *Runtime
	runtime, err := New(loadConfigForDirective(t), func(_ string, _ config.AgentConfig) (provider.Provider, error) {
		return provider.NewMockHandler(func(context.Context, provider.Request) (provider.Response, error) {
			runtime.mu.Lock()
			runtime.status.ID = "changed-run-id"
			runtime.mu.Unlock()
			return provider.Response{Content: "operator response"}, nil
		}), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Chat(context.Background(), "Explain current research state", false); err != nil {
		t.Fatal(err)
	}
	messages := runtime.Messages()
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[1].RunID != messages[0].RunID {
		t.Fatalf("response run ID = %q, request run ID = %q", messages[1].RunID, messages[0].RunID)
	}
}
