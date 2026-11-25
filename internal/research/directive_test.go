package research

import (
	"context"
	"testing"

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
