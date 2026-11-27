package ratelimit_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/ratelimit"
)

func TestPacerSpacesConcurrentWaiters(t *testing.T) {
	var pacer ratelimit.Pacer
	const waiters = 4
	const interval = 40 * time.Millisecond
	start := make(chan struct{})
	ready := sync.WaitGroup{}
	ready.Add(waiters)
	results := make(chan time.Time, waiters)
	for range waiters {
		go func() {
			ready.Done()
			<-start
			if err := pacer.Wait(context.Background(), interval); err != nil {
				t.Errorf("Wait() error = %v", err)
			}
			results <- time.Now()
		}()
	}
	ready.Wait()
	close(start)
	completions := make([]time.Time, 0, waiters)
	for range waiters {
		completions = append(completions, <-results)
	}
	sort.Slice(completions, func(i, j int) bool { return completions[i].Before(completions[j]) })
	for i := 1; i < len(completions); i++ {
		if gap := completions[i].Sub(completions[i-1]); gap < 30*time.Millisecond {
			t.Fatalf("completion %d followed previous completion by %s", i, gap)
		}
	}
}

func TestPacerExpiresIdleSlots(t *testing.T) {
	var pacer ratelimit.Pacer
	const interval = 60 * time.Millisecond
	if err := pacer.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * interval)
	started := time.Now()
	if err := pacer.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= interval {
		t.Fatalf("expired slot caused a %s wait", elapsed)
	}
	started = time.Now()
	if err := pacer.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond {
		t.Fatalf("wait after expired slot did not reserve a fresh interval: %s", elapsed)
	}
}

func TestPacerCancellationConsumesReservedSlot(t *testing.T) {
	var pacer ratelimit.Pacer
	const interval = 60 * time.Millisecond
	if err := pacer.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := pacer.Wait(ctx, interval); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
	started := time.Now()
	if err := pacer.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 90*time.Millisecond {
		t.Fatalf("canceled wait did not consume its slot; next wait = %s", elapsed)
	}
}

func TestPacerInstancesAreIndependent(t *testing.T) {
	var first, second ratelimit.Pacer
	const interval = 80 * time.Millisecond
	if err := first.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := second.Wait(context.Background(), interval); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= interval/2 {
		t.Fatalf("independent pacer waited %s", elapsed)
	}
}

func TestPacerConcurrentWaitRaceSafety(t *testing.T) {
	var pacer ratelimit.Pacer
	const callers = 16
	const callsPerCaller = 8
	wait := sync.WaitGroup{}
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			for range callsPerCaller {
				if err := pacer.Wait(context.Background(), time.Microsecond); err != nil {
					t.Errorf("Wait() error = %v", err)
				}
			}
		}()
	}
	wait.Wait()
}
