package budget_test

import (
	"context"
	"errors"
	"reflect"
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

func TestRestoreTreatsOpenReservationsAsUncertain(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 200})
	confirmed, _ := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 50})
	if err := confirmed.Reconcile(budget.Usage{CostMicros: 40, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reserve(context.Background(), budget.Estimate{Provider: "openai", CostMicros: 20, Requests: 1}); err != nil {
		t.Fatal(err)
	}
	status := manager.Status()
	if len(status.OpenReservations) != 1 {
		t.Fatalf("open reservations = %#v", status.OpenReservations)
	}
	restored := budget.NewManager(budget.Limits{HardCostMicros: 200})
	restored.RestoreStatus(status)
	recovered := restored.Status()
	if recovered.Uncertain.CostMicros != 20 || recovered.Confirmed.CostMicros != 40 || len(recovered.OpenReservations) != 0 {
		t.Fatalf("recovered = %#v", recovered)
	}
	if recovered.Providers["openai"].Uncertain.CostMicros != 20 {
		t.Fatalf("recovered provider = %#v", recovered.Providers["openai"])
	}
}

func TestPersistenceHookRunsAfterReservationAndSettlement(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 100})
	var snapshots []budget.Status
	manager.SetPersistence(func(status budget.Status) error {
		snapshots = append(snapshots, status)
		return nil
	})
	reservation, err := manager.Reserve(context.Background(), budget.Estimate{CostMicros: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := reservation.Reconcile(budget.Usage{CostMicros: 5}); err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Reserved.CostMicros != 10 || snapshots[1].Confirmed.CostMicros != 5 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func TestReservationAccountParity(t *testing.T) {
	reserved := usageForParityTest(10)
	confirmed := usageForParityTest(100)
	tests := []struct {
		name   string
		settle func(budget.Reservation) error
		want   budget.AccountStatus
	}{
		{
			name: "reconcile",
			settle: func(reservation budget.Reservation) error {
				return reservation.Reconcile(confirmed)
			},
			want: budget.AccountStatus{Confirmed: confirmed},
		},
		{
			name:   "release success",
			settle: func(reservation budget.Reservation) error { return reservation.Release(nil) },
			want:   budget.AccountStatus{},
		},
		{
			name: "release error",
			settle: func(reservation budget.Reservation) error {
				return reservation.Release(errors.New("interrupted"))
			},
			want: budget.AccountStatus{Uncertain: reserved},
		},
	}
	for _, provider := range []string{"", "openai"} {
		for _, test := range tests {
			name := provider + "/" + test.name
			if provider == "" {
				name = "no provider/" + test.name
			}
			t.Run(name, func(t *testing.T) {
				manager := budget.NewManager(budget.Limits{HardCostMicros: 10000})
				var snapshots []budget.Status
				manager.SetPersistence(func(status budget.Status) error {
					snapshots = append(snapshots, status)
					return nil
				})
				estimate := estimateForParityTest(provider, 10)
				reservation := reserveForParityTest(t, manager, estimate)

				status := manager.Status()
				requireAccountParity(t, status, provider, budget.AccountStatus{Reserved: reserved})
				if len(status.OpenReservations) != 1 || status.OpenReservations[0].Estimate != estimate {
					t.Fatalf("open reservations = %#v", status.OpenReservations)
				}
				if len(snapshots) != 1 {
					t.Fatalf("snapshots after reserve = %#v", snapshots)
				}
				requireAccountParity(t, snapshots[0], provider, budget.AccountStatus{Reserved: reserved})

				if err := test.settle(reservation); err != nil {
					t.Fatal(err)
				}
				status = manager.Status()
				requireAccountParity(t, status, provider, test.want)
				if len(status.OpenReservations) != 0 {
					t.Fatalf("open reservations after settlement = %#v", status.OpenReservations)
				}
				if len(snapshots) != 2 {
					t.Fatalf("snapshots after settlement = %#v", snapshots)
				}
				requireAccountParity(t, snapshots[1], provider, test.want)
			})
		}
	}
}

func TestMultipleProviderAccountsRemainIndependent(t *testing.T) {
	manager := budget.NewManager(budget.Limits{HardCostMicros: 10000})
	openAIUsage := usageForParityTest(10)
	mockUsage := usageForParityTest(20)
	closedOpenAIUsage := usageForParityTest(30)
	globalOnlyUsage := usageForParityTest(40)

	openAI := reserveForParityTest(t, manager, estimateForParityTest("openai", 10))
	mock := reserveForParityTest(t, manager, estimateForParityTest("mock", 20))
	closedOpenAI := reserveForParityTest(t, manager, estimateForParityTest("openai", 30))
	globalOnly := reserveForParityTest(t, manager, estimateForParityTest("", 40))

	status := manager.Status()
	if status.Reserved != sumForParityTest(openAIUsage, mockUsage, closedOpenAIUsage, globalOnlyUsage) {
		t.Fatalf("global reserved = %#v", status.Reserved)
	}
	if got := status.Providers["openai"].Reserved; got != sumForParityTest(openAIUsage, closedOpenAIUsage) {
		t.Fatalf("openai reserved = %#v", got)
	}
	if got := status.Providers["mock"].Reserved; got != mockUsage {
		t.Fatalf("mock reserved = %#v", got)
	}

	confirmedOpenAI := usageForParityTest(100)
	confirmedGlobalOnly := usageForParityTest(200)
	if err := openAI.Reconcile(confirmedOpenAI); err != nil {
		t.Fatal(err)
	}
	if err := mock.Release(errors.New("interrupted")); err != nil {
		t.Fatal(err)
	}
	if err := closedOpenAI.Release(nil); err != nil {
		t.Fatal(err)
	}
	if err := globalOnly.Reconcile(confirmedGlobalOnly); err != nil {
		t.Fatal(err)
	}

	status = manager.Status()
	wantGlobal := budget.AccountStatus{
		Confirmed: sumForParityTest(confirmedOpenAI, confirmedGlobalOnly),
		Uncertain: mockUsage,
	}
	gotGlobal := budget.AccountStatus{Confirmed: status.Confirmed, Uncertain: status.Uncertain, Reserved: status.Reserved}
	if gotGlobal != wantGlobal {
		t.Fatalf("global account = %#v, want %#v", gotGlobal, wantGlobal)
	}
	wantProviders := map[string]budget.AccountStatus{
		"openai": {Confirmed: confirmedOpenAI},
		"mock":   {Uncertain: mockUsage},
	}
	if len(status.Providers) != len(wantProviders) {
		t.Fatalf("providers = %#v", status.Providers)
	}
	for provider, want := range wantProviders {
		if got := status.Providers[provider]; got != want {
			t.Errorf("provider %s = %#v, want %#v", provider, got, want)
		}
	}
}

func TestReservePersistenceFailureRollsBack(t *testing.T) {
	for _, provider := range []string{"", "openai"} {
		name := provider
		if provider == "" {
			name = "no provider"
		}
		t.Run(name, func(t *testing.T) {
			manager := budget.NewManager(budget.Limits{HardCostMicros: 10000})
			before := manager.Status()
			persistenceErr := errors.New("persistence unavailable")
			calls := 0
			manager.SetPersistence(func(budget.Status) error {
				calls++
				if calls == 1 {
					return persistenceErr
				}
				return nil
			})
			estimate := estimateForParityTest(provider, 10)
			reservation, err := manager.Reserve(context.Background(), estimate)
			if !errors.Is(err, persistenceErr) {
				t.Fatalf("reserve error = %v, want %v", err, persistenceErr)
			}
			if reservation.ID() != "" {
				t.Errorf("failed reservation ID = %q", reservation.ID())
			}
			if err := reservation.Release(nil); !errors.Is(err, budget.ErrReservationClosed) {
				t.Errorf("zero reservation release error = %v", err)
			}
			if got := manager.Status(); !reflect.DeepEqual(got, before) {
				t.Errorf("status after failed reserve = %#v, want %#v", got, before)
			}

			reservation = reserveForParityTest(t, manager, estimate)
			if reservation.ID() != "reservation-1" {
				t.Errorf("retry reservation ID = %q, want reservation-1", reservation.ID())
			}
			requireAccountParity(t, manager.Status(), provider, budget.AccountStatus{Reserved: usageForParityTest(10)})
			if err := reservation.Release(nil); err != nil {
				t.Fatalf("release after reserve retry: %v", err)
			}
			requireAccountParity(t, manager.Status(), provider, budget.AccountStatus{})
			if calls != 3 {
				t.Errorf("persistence calls = %d, want 3", calls)
			}
		})
	}
}

func TestSettlementPersistenceFailureRollsBackAndHandleRemainsUsable(t *testing.T) {
	confirmed := usageForParityTest(100)
	uncertain := usageForParityTest(10)
	interrupted := errors.New("interrupted")
	tests := []struct {
		name   string
		settle func(budget.Reservation) error
		retry  func(budget.Reservation) error
		want   budget.AccountStatus
	}{
		{
			name: "reconcile retry",
			settle: func(reservation budget.Reservation) error {
				return reservation.Reconcile(confirmed)
			},
			retry: func(reservation budget.Reservation) error {
				return reservation.Reconcile(confirmed)
			},
			want: budget.AccountStatus{Confirmed: confirmed},
		},
		{
			name: "reconcile then deferred release",
			settle: func(reservation budget.Reservation) error {
				return reservation.Reconcile(confirmed)
			},
			retry: func(reservation budget.Reservation) error { return reservation.Release(nil) },
			want:  budget.AccountStatus{},
		},
		{
			name: "uncertain release retry",
			settle: func(reservation budget.Reservation) error {
				return reservation.Release(interrupted)
			},
			retry: func(reservation budget.Reservation) error {
				return reservation.Release(interrupted)
			},
			want: budget.AccountStatus{Uncertain: uncertain},
		},
		{
			name: "uncertain release then deferred release",
			settle: func(reservation budget.Reservation) error {
				return reservation.Release(interrupted)
			},
			retry: func(reservation budget.Reservation) error { return reservation.Release(nil) },
			want:  budget.AccountStatus{},
		},
	}
	for _, provider := range []string{"", "openai"} {
		for _, test := range tests {
			providerName := provider
			if provider == "" {
				providerName = "no provider"
			}
			t.Run(providerName+"/"+test.name, func(t *testing.T) {
				manager := budget.NewManager(budget.Limits{HardCostMicros: 10000})
				persistenceErr := errors.New("persistence unavailable")
				calls := 0
				manager.SetPersistence(func(budget.Status) error {
					calls++
					if calls == 2 {
						return persistenceErr
					}
					return nil
				})
				reservation := reserveForParityTest(t, manager, estimateForParityTest(provider, 10))
				before := manager.Status()

				if err := test.settle(reservation); !errors.Is(err, persistenceErr) {
					t.Fatalf("settlement error = %v, want %v", err, persistenceErr)
				}
				if got := manager.Status(); !reflect.DeepEqual(got, before) {
					t.Errorf("status after failed settlement = %#v, want %#v", got, before)
				}

				if err := test.retry(reservation); err != nil {
					t.Fatalf("reservation retry/release after persistence failure: %v", err)
				}
				status := manager.Status()
				requireAccountParity(t, status, provider, test.want)
				if len(status.OpenReservations) != 0 {
					t.Errorf("open reservations after retry = %#v", status.OpenReservations)
				}
				if err := reservation.Release(nil); !errors.Is(err, budget.ErrReservationClosed) {
					t.Errorf("release after successful settlement = %v", err)
				}
				if calls != 3 {
					t.Errorf("persistence calls = %d, want 3", calls)
				}
			})
		}
	}
}

func TestRestoreStatusTransfersOpenReservationsToUncertain(t *testing.T) {
	for _, provider := range []string{"", "openai"} {
		name := provider
		if provider == "" {
			name = "no provider"
		}
		t.Run(name, func(t *testing.T) {
			recoveredUsage := usageForParityTest(10)
			residualReserved := usageForParityTest(40)
			confirmed := usageForParityTest(100)
			uncertain := usageForParityTest(200)
			accountSnapshot := budget.AccountStatus{
				Confirmed: confirmed,
				Uncertain: uncertain,
				Reserved:  sumForParityTest(recoveredUsage, residualReserved),
			}
			snapshot := budget.Status{
				Phase:     budget.PhaseFinalizing,
				Confirmed: accountSnapshot.Confirmed,
				Uncertain: accountSnapshot.Uncertain,
				Reserved:  accountSnapshot.Reserved,
				OpenReservations: []budget.ReservationStatus{
					{ID: "reservation-7", Estimate: estimateForParityTest(provider, 10)},
				},
			}
			if provider != "" {
				snapshot.Providers = map[string]budget.AccountStatus{provider: accountSnapshot}
			}
			restored := budget.NewManager(budget.Limits{HardCostMicros: 10000})
			restored.RestoreStatus(snapshot)
			status := restored.Status()
			want := budget.AccountStatus{
				Confirmed: confirmed,
				Uncertain: sumForParityTest(uncertain, recoveredUsage),
				Reserved:  residualReserved,
			}
			requireAccountParity(t, status, provider, want)
			if len(status.OpenReservations) != 0 {
				t.Errorf("open reservations = %#v", status.OpenReservations)
			}
			beforeTotal := sumForParityTest(snapshot.Confirmed, snapshot.Uncertain, snapshot.Reserved)
			afterTotal := sumForParityTest(status.Confirmed, status.Uncertain, status.Reserved)
			if afterTotal != beforeTotal {
				t.Errorf("accounted total after recovery = %#v, want %#v", afterTotal, beforeTotal)
			}
			wantRemaining := int64(10000) - beforeTotal.CostMicros
			if status.EstimatedRemainingCostMicros != wantRemaining {
				t.Errorf("estimated remaining = %d, want %d", status.EstimatedRemainingCostMicros, wantRemaining)
			}

			restored.RestoreStatus(snapshot)
			if repeated := restored.Status(); !reflect.DeepEqual(repeated, status) {
				t.Errorf("repeated snapshot recovery = %#v, want stable %#v", repeated, status)
			}
			restored.RestoreStatus(status)
			if repeated := restored.Status(); !reflect.DeepEqual(repeated, status) {
				t.Errorf("recovery of recovered snapshot = %#v, want stable %#v", repeated, status)
			}
		})
	}
}

func TestRestoreStatusMissingProviderDoesNotCreateNegativeReserved(t *testing.T) {
	firstRecovery := usageForParityTest(10)
	secondRecovery := usageForParityTest(20)
	residualReserved := usageForParityTest(40)
	confirmed := usageForParityTest(100)
	uncertain := usageForParityTest(200)
	snapshot := budget.Status{
		Confirmed: confirmed,
		Uncertain: uncertain,
		Reserved:  sumForParityTest(firstRecovery, secondRecovery, residualReserved),
		OpenReservations: []budget.ReservationStatus{
			{ID: "reservation-1", Estimate: estimateForParityTest("openai", 10)},
			{ID: "reservation-2", Estimate: estimateForParityTest("openai", 20)},
		},
	}
	restored := budget.NewManager(budget.Limits{HardCostMicros: 10000})
	restored.RestoreStatus(snapshot)
	status := restored.Status()

	wantGlobal := budget.AccountStatus{
		Confirmed: confirmed,
		Uncertain: sumForParityTest(uncertain, firstRecovery, secondRecovery),
		Reserved:  residualReserved,
	}
	gotGlobal := budget.AccountStatus{Confirmed: status.Confirmed, Uncertain: status.Uncertain, Reserved: status.Reserved}
	if gotGlobal != wantGlobal {
		t.Errorf("global account = %#v, want %#v", gotGlobal, wantGlobal)
	}
	provider, ok := status.Providers["openai"]
	if !ok {
		t.Fatal("recovered provider account is missing")
	}
	wantProvider := budget.AccountStatus{Uncertain: sumForParityTest(firstRecovery, secondRecovery)}
	if provider != wantProvider {
		t.Errorf("provider account = %#v, want nonnegative residual %#v", provider, wantProvider)
	}
	beforeTotal := sumForParityTest(snapshot.Confirmed, snapshot.Uncertain, snapshot.Reserved)
	afterTotal := sumForParityTest(status.Confirmed, status.Uncertain, status.Reserved)
	if afterTotal != beforeTotal {
		t.Errorf("accounted total = %#v, want %#v", afterTotal, beforeTotal)
	}
}

func TestRestoreStatusMultipleProvidersPreserveResidualReserved(t *testing.T) {
	existingRecovery := usageForParityTest(10)
	firstMissingRecovery := usageForParityTest(20)
	secondMissingRecovery := usageForParityTest(30)
	globalRecovery := usageForParityTest(40)
	globalResidual := usageForParityTest(50)
	existingResidual := usageForParityTest(60)
	confirmed := usageForParityTest(100)
	uncertain := usageForParityTest(200)
	existingUncertain := usageForParityTest(300)
	snapshot := budget.Status{
		Confirmed: confirmed,
		Uncertain: uncertain,
		Reserved: sumForParityTest(
			existingRecovery,
			firstMissingRecovery,
			secondMissingRecovery,
			globalRecovery,
			globalResidual,
		),
		Providers: map[string]budget.AccountStatus{
			"openai": {
				Uncertain: existingUncertain,
				Reserved:  sumForParityTest(existingRecovery, existingResidual),
			},
		},
		OpenReservations: []budget.ReservationStatus{
			{ID: "reservation-1", Estimate: estimateForParityTest("openai", 10)},
			{ID: "reservation-2", Estimate: estimateForParityTest("mock", 20)},
			{ID: "reservation-3", Estimate: estimateForParityTest("mock", 30)},
			{ID: "reservation-4", Estimate: estimateForParityTest("", 40)},
		},
	}
	restored := budget.NewManager(budget.Limits{HardCostMicros: 10000})
	restored.RestoreStatus(snapshot)
	status := restored.Status()

	wantGlobal := budget.AccountStatus{
		Confirmed: confirmed,
		Uncertain: sumForParityTest(uncertain, existingRecovery, firstMissingRecovery, secondMissingRecovery, globalRecovery),
		Reserved:  globalResidual,
	}
	gotGlobal := budget.AccountStatus{Confirmed: status.Confirmed, Uncertain: status.Uncertain, Reserved: status.Reserved}
	if gotGlobal != wantGlobal {
		t.Errorf("global account = %#v, want %#v", gotGlobal, wantGlobal)
	}
	wantProviders := map[string]budget.AccountStatus{
		"openai": {
			Uncertain: sumForParityTest(existingUncertain, existingRecovery),
			Reserved:  existingResidual,
		},
		"mock": {Uncertain: sumForParityTest(firstMissingRecovery, secondMissingRecovery)},
	}
	if len(status.Providers) != len(wantProviders) {
		t.Fatalf("providers = %#v", status.Providers)
	}
	for provider, want := range wantProviders {
		if got := status.Providers[provider]; got != want {
			t.Errorf("provider %s = %#v, want %#v", provider, got, want)
		}
	}
	if len(status.OpenReservations) != 0 {
		t.Errorf("open reservations = %#v", status.OpenReservations)
	}
	beforeTotal := sumForParityTest(snapshot.Confirmed, snapshot.Uncertain, snapshot.Reserved)
	afterTotal := sumForParityTest(status.Confirmed, status.Uncertain, status.Reserved)
	if afterTotal != beforeTotal {
		t.Errorf("accounted total = %#v, want %#v", afterTotal, beforeTotal)
	}
}

func reserveForParityTest(t *testing.T, manager *budget.Manager, estimate budget.Estimate) budget.Reservation {
	t.Helper()
	reservation, err := manager.Reserve(context.Background(), estimate)
	if err != nil {
		t.Fatal(err)
	}
	return reservation
}

func requireAccountParity(t *testing.T, status budget.Status, provider string, want budget.AccountStatus) {
	t.Helper()
	global := budget.AccountStatus{Confirmed: status.Confirmed, Uncertain: status.Uncertain, Reserved: status.Reserved}
	if global != want {
		t.Fatalf("global account = %#v, want %#v", global, want)
	}
	if provider == "" {
		if len(status.Providers) != 0 {
			t.Fatalf("providers for global-only reservation = %#v", status.Providers)
		}
		return
	}
	providerAccount, ok := status.Providers[provider]
	if !ok {
		t.Fatalf("provider %s is missing from %#v", provider, status.Providers)
	}
	if providerAccount != global {
		t.Fatalf("provider account = %#v, global account = %#v", providerAccount, global)
	}
}

func usageForParityTest(base int64) budget.Usage {
	return budget.Usage{
		CostMicros:      base,
		InputTokens:     base + 1,
		OutputTokens:    base + 2,
		ReasoningTokens: base + 3,
		CachedTokens:    base + 4,
		Requests:        base + 5,
	}
}

func estimateForParityTest(provider string, base int64) budget.Estimate {
	return budget.Estimate{
		Provider:        provider,
		CostMicros:      base,
		InputTokens:     base + 1,
		OutputTokens:    base + 2,
		ReasoningTokens: base + 3,
		CachedTokens:    base + 4,
		Requests:        base + 5,
	}
}

func sumForParityTest(values ...budget.Usage) budget.Usage {
	var result budget.Usage
	for _, value := range values {
		result.CostMicros += value.CostMicros
		result.InputTokens += value.InputTokens
		result.OutputTokens += value.OutputTokens
		result.ReasoningTokens += value.ReasoningTokens
		result.CachedTokens += value.CachedTokens
		result.Requests += value.Requests
	}
	return result
}
