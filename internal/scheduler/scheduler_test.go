package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

func TestDependenciesGateReadyTasks(t *testing.T) {
	s := scheduler.New(2, time.Minute)
	submit(t, s, scheduler.Task{ID: "a", Role: "blocker"})
	submit(t, s, scheduler.Task{ID: "b", Role: "worker", Dependencies: []string{"a"}})
	assertNoLease(t, s)
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "w1", Roles: []string{"blocker"}})
	if err != nil || lease.Task.ID != "a" {
		t.Fatalf("Lease() = %#v, %v", lease, err)
	}
	if err := s.Complete(lease.ID, lease.FencingToken, scheduler.Result{Output: "done"}); err != nil {
		t.Fatal(err)
	}
	lease, err = s.Lease(context.Background(), scheduler.Worker{ID: "w1", Roles: []string{"worker"}})
	if err != nil || lease.Task.ID != "b" {
		t.Fatalf("dependent Lease() = %#v, %v", lease, err)
	}
}

func TestHigherPriorityLeasesFirst(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "low", Role: "worker", Priority: 1, CreatedAt: time.Unix(1, 0)})
	submit(t, s, scheduler.Task{ID: "high", Role: "worker", Priority: 10, CreatedAt: time.Unix(2, 0)})
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err != nil || lease.Task.ID != "high" {
		t.Fatalf("Lease() = %#v, %v", lease, err)
	}
}

func TestDuplicateIdempotencyKeyIsRejected(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "one", Role: "worker", IdempotencyKey: "same"})
	if err := s.Submit(scheduler.Task{ID: "two", Role: "worker", IdempotencyKey: "same"}); !errors.Is(err, scheduler.ErrDuplicateTask) {
		t.Fatalf("Submit() error = %v", err)
	}
}

func TestCapacityProvidesBackpressure(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "one", Role: "worker"})
	submit(t, s, scheduler.Task{ID: "two", Role: "worker"})
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	assertNoLease(t, s)
	if err := s.Complete(lease.ID, lease.FencingToken, scheduler.Result{}); err != nil {
		t.Fatal(err)
	}
	lease, err = s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err != nil || lease.Task.ID != "two" {
		t.Fatalf("Lease() after completion = %#v, %v", lease, err)
	}
}

func TestExpiredLeaseRecoversAndFencesOldOwner(t *testing.T) {
	s := scheduler.New(1, time.Millisecond)
	submit(t, s, scheduler.Task{ID: "task", Role: "worker", MaxAttempts: 2})
	old, err := s.Lease(context.Background(), scheduler.Worker{ID: "old", Roles: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if recovered := s.Recover(time.Now()); recovered != 1 {
		t.Fatalf("recovered = %d", recovered)
	}
	current, err := s.Lease(context.Background(), scheduler.Worker{ID: "new", Roles: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if current.FencingToken <= old.FencingToken {
		t.Fatalf("fencing did not increase: %d -> %d", old.FencingToken, current.FencingToken)
	}
	if err := s.Complete(old.ID, old.FencingToken, scheduler.Result{}); !errors.Is(err, scheduler.ErrStaleLease) {
		t.Fatalf("stale completion error = %v", err)
	}
	if err := s.Complete(current.ID, current.FencingToken, scheduler.Result{Output: "current"}); err != nil {
		t.Fatal(err)
	}
	task, ok := s.Get("task")
	if !ok || task.State != scheduler.StateCompleted || task.Result.Output != "current" {
		t.Fatalf("task = %#v, %v", task, ok)
	}
}

func TestRetryAndPermanentFailure(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "task", Role: "worker", MaxAttempts: 2})
	first, _ := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err := s.Fail(first.ID, first.FencingToken, errors.New("temporary"), true); err != nil {
		t.Fatal(err)
	}
	second, err := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(second.ID, second.FencingToken, errors.New("permanent"), true); err != nil {
		t.Fatal(err)
	}
	task, _ := s.Get("task")
	if task.State != scheduler.StateFailed || task.Attempts != 2 {
		t.Fatalf("task = %#v", task)
	}
}

func TestCancellationRejectsLeaseAndStaleResult(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "task", Role: "worker"})
	lease, _ := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
	if err := s.Cancel("task"); err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(lease.ID, lease.FencingToken, scheduler.Result{}); !errors.Is(err, scheduler.ErrStaleLease) {
		t.Fatalf("completion error = %v", err)
	}
}

func TestConcurrentLeasesAreUnique(t *testing.T) {
	s := scheduler.New(5, time.Minute)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		submit(t, s, scheduler.Task{ID: id, Role: "worker"})
	}
	var wait sync.WaitGroup
	var leased atomic.Int32
	ids := make(chan string, 5)
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "w", Roles: []string{"worker"}})
			if err == nil {
				leased.Add(1)
				ids <- lease.Task.ID
			} else if !errors.Is(err, scheduler.ErrNoReadyTask) {
				t.Errorf("Lease() error = %v", err)
			}
		}()
	}
	wait.Wait()
	close(ids)
	if leased.Load() != 5 {
		t.Fatalf("leased = %d", leased.Load())
	}
	seen := map[string]bool{}
	for id := range ids {
		if seen[id] {
			t.Fatalf("task %s leased more than once", id)
		}
		seen[id] = true
	}
}

func TestLeaseHonorsContext(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Lease(ctx, scheduler.Worker{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func submit(t *testing.T, s *scheduler.Scheduler, task scheduler.Task) {
	t.Helper()
	if err := s.Submit(task); err != nil {
		t.Fatal(err)
	}
}

func assertNoLease(t *testing.T, s *scheduler.Scheduler) {
	t.Helper()
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "none", Roles: []string{"worker"}})
	if lease != nil || !errors.Is(err, scheduler.ErrNoReadyTask) {
		t.Fatalf("Lease() = %#v, %v, want ErrNoReadyTask", lease, err)
	}
}

func TestRestoreTasksPreservesResultsAndRecoversRunningLeases(t *testing.T) {
	s := scheduler.New(2, time.Minute)
	oldFence := uint64(7)
	if err := s.RestoreTasks([]scheduler.Task{
		{ID: "done", Role: "worker", State: scheduler.StateCompleted, IdempotencyKey: "done-key", Result: scheduler.Result{Output: "kept"}},
		{ID: "interrupted", Role: "worker", State: scheduler.StateRunning, Attempts: 1, MaxAttempts: 1, FencingToken: oldFence},
	}); err != nil {
		t.Fatal(err)
	}
	completed, _ := s.Get("done")
	if completed.Result.Output != "kept" || completed.State != scheduler.StateCompleted {
		t.Fatalf("completed = %#v", completed)
	}
	if err := s.Submit(scheduler.Task{ID: "duplicate", Role: "worker", IdempotencyKey: "done-key"}); !errors.Is(err, scheduler.ErrDuplicateTask) {
		t.Fatalf("duplicate restore key error = %v", err)
	}
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "new", Roles: []string{"worker"}})
	if err != nil || lease.Task.ID != "interrupted" || lease.FencingToken <= oldFence {
		t.Fatalf("restored lease = %#v, %v", lease, err)
	}
}

func TestHeartbeatExtendsActiveLease(t *testing.T) {
	s := scheduler.New(1, 20*time.Millisecond)
	submit(t, s, scheduler.Task{ID: "task", Role: "worker"})
	lease, _ := s.Lease(context.Background(), scheduler.Worker{ID: "worker", Roles: []string{"worker"}})
	extended := lease.ExpiresAt.Add(time.Hour)
	if err := s.Heartbeat(lease.ID, lease.FencingToken, extended); err != nil {
		t.Fatal(err)
	}
	if recovered := s.Recover(lease.ExpiresAt.Add(time.Nanosecond)); recovered != 0 {
		t.Fatalf("heartbeat did not extend lease: %d", recovered)
	}
	if err := s.Heartbeat(lease.ID, lease.FencingToken+1, extended); !errors.Is(err, scheduler.ErrStaleLease) {
		t.Fatalf("stale heartbeat error = %v", err)
	}
}

func TestReprioritizeChangesLeaseOrder(t *testing.T) {
	s := scheduler.New(1, time.Minute)
	submit(t, s, scheduler.Task{ID: "low", Role: "worker", Priority: 1})
	submit(t, s, scheduler.Task{ID: "high", Role: "worker", Priority: 2})
	if err := s.Reprioritize("low", 10); err != nil {
		t.Fatal(err)
	}
	lease, err := s.Lease(context.Background(), scheduler.Worker{ID: "worker", Roles: []string{"worker"}})
	if err != nil || lease.Task.ID != "low" {
		t.Fatalf("Lease() = %#v, %v", lease, err)
	}
	if err := s.Reprioritize("missing", 1); !errors.Is(err, scheduler.ErrTaskNotFound) {
		t.Fatalf("error = %v", err)
	}
}
