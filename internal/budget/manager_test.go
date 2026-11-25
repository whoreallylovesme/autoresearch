package budget_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/budget"
)

func TestReservationHonorsFinalizationReserve(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 1000, FinalizationReserveMicros: 200})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 800})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Reconcile(budget.Usage{CostMicros: 800, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 1}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("normal request error = %v", err)
	}
	final, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 200, Finalization: true})
	if err != nil {
		t.Fatalf("finalization reservation error = %v", err)
	}
	if err := final.Reconcile(budget.Usage{CostMicros: 200, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if status.Confirmed.CostMicros != 1000 || status.Reserved.CostMicros != 0 {
		t.Fatalf("status = %#v", status)
	}
}

func TestSoftLimitStartsFinalization(t *testing.T) {
	manager := budget.NewManager(budget.Limits{SoftCostMicros: 500, HardCostMicros: 1000, FinalizationReserveMicros: 100})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 500})
	if err != nil {
		t.Fatal(err)
	}
	if manager.Status().Phase != budget.PhaseFinalizing {
		t.Fatalf("phase = %s", manager.Status().Phase)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 0}); !errors.Is(err, budget.ErrSoftLimit) {
		t.Fatalf("new research error = %v", err)
	}
	if err := reservation.Reconcile(budget.Usage{CostMicros: 450, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 100, Finalization: true}); err != nil {
		t.Fatalf("finalization error = %v", err)
	}
}

func TestConcurrentReservationsCannotOverspend(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 550})
	var accepted atomic.Int32
	var rejected atomic.Int32
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 10})
			if err == nil {
				accepted.Add(1)
			} else if errors.Is(err, budget.ErrHardLimit) {
				rejected.Add(1)
			} else {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wait.Wait()
	if accepted.Load() != 55 || rejected.Load() != 45 {
		t.Fatalf("accepted = %d, rejected = %d", accepted.Load(), rejected.Load())
	}
	if got := manager.Status().Reserved.CostMicros; got != 550 {
		t.Fatalf("reserved = %d", got)
	}
}

func TestReconcileIsSingleUse(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 100})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Reconcile(budget.Usage{CostMicros: 70}); err != nil {
		t.Fatal(err)
	}
	if err := reservation.Reconcile(budget.Usage{CostMicros: 70}); !errors.Is(err, budget.ErrReservationClosed) {
		t.Fatalf("duplicate reconcile error = %v", err)
	}
	if manager.Status().Confirmed.CostMicros != 70 {
		t.Fatalf("confirmed = %#v", manager.Status().Confirmed)
	}
}

func TestInterruptedRequestUsesConservativeEstimate(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 100})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 100, InputTokens: 5})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Release(errors.New("connection lost")); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if status.Uncertain.CostMicros != 100 || status.Confirmed.CostMicros != 0 {
		t.Fatalf("status = %#v", status)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 1}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("reservation after uncertainty = %v", err)
	}
}

func TestTokenAndRequestLimitsAreIndependent(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 0, MaxInputTokens: 10, MaxRequests: 1})
	if _, err := manager.Reserve(context.Background(), budget.Estimate{InputTokens: 11}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("token limit error = %v", err)
	}
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{InputTokens: 10, Requests: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Reconcile(budget.Usage{InputTokens: 4, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{Requests: 1}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("request limit error = %v", err)
	}
}

func TestProviderQuotaIsIndependent(t *testing.T) {
	manager := budget.NewManager(budget.Limits{
		HardCostMicros: 100,
		Providers: map[string]budget.ProviderLimits{
			"openai": {MaxRequests: 1},
			"mock":   {MaxRequests: 2},
		},
	})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{Provider: "openai", Requests: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{Provider: "openai", Requests: 1}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("openai quota error = %v", err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{Provider: "mock", Requests: 1}); err != nil {
		t.Fatalf("independent mock quota error = %v", err)
	}
	if err := reservation.Release(nil); err != nil {
		t.Fatal(err)
	}
}

func TestZeroCostMockCanRunWithZeroMonetaryBudget(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 0})
	if _, err := manager.Reserve(context.Background(), budget.Estimate{}); err != nil {
		t.Fatalf("zero-cost reservation error = %v", err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 1}); !errors.Is(err, budget.ErrHardLimit) {
		t.Fatalf("paid reservation error = %v", err)
	}
}

func TestReserveHonorsCanceledContext(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 100})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.Reserve(ctx, budget.Estimate{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
