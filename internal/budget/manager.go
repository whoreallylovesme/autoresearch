package budget

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrSoftLimit         = errors.New("budget soft limit reached")
	ErrHardLimit         = errors.New("budget hard limit reached")
	ErrReservationClosed = errors.New("budget reservation is already closed")
)

type Phase string

const (
	PhaseRunning    Phase = "running"
	PhaseFinalizing Phase = "finalizing"
)

type Usage struct {
	CostMicros      int64 `json:"cost_micros"`
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	Requests        int64 `json:"requests"`
}

type Estimate struct {
	Provider        string
	CostMicros      int64
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
	CachedTokens    int64
	Requests        int64
	Finalization    bool
}

type ProviderLimits struct {
	HardCostMicros int64
	MaxRequests    int64
}

type Limits struct {
	SoftCostMicros            int64
	HardCostMicros            int64
	FinalizationReserveMicros int64
	MaxInputTokens            int64
	MaxOutputTokens           int64
	MaxReasoningTokens        int64
	MaxCachedTokens           int64
	MaxRequests               int64
	Providers                 map[string]ProviderLimits
}

type AccountStatus struct {
	Confirmed Usage `json:"confirmed"`
	Uncertain Usage `json:"uncertain"`
	Reserved  Usage `json:"reserved"`
}

type Status struct {
	Phase                        Phase                    `json:"phase"`
	Confirmed                    Usage                    `json:"confirmed"`
	Uncertain                    Usage                    `json:"uncertain"`
	Reserved                     Usage                    `json:"reserved"`
	EstimatedRemainingCostMicros int64                    `json:"estimated_remaining_cost_micros"`
	Providers                    map[string]AccountStatus `json:"providers,omitempty"`
}

type account struct {
	confirmed Usage
	uncertain Usage
	reserved  Usage
}

type Manager struct {
	mu           sync.Mutex
	limits       Limits
	phase        Phase
	account      account
	providers    map[string]*account
	reservations map[string]Estimate
	nextID       uint64
}

type Reservation struct {
	id       string
	manager  *Manager
	estimate Estimate
}

func NewManager(limits Limits) *Manager {
	return &Manager{
		limits: limits, phase: PhaseRunning, providers: map[string]*account{}, reservations: map[string]Estimate{},
	}
}

func (m *Manager) Reserve(ctx context.Context, estimate Estimate) (Reservation, error) {
	if err := ctx.Err(); err != nil {
		return Reservation{}, err
	}
	if err := validUsage(estimate.usage()); err != nil {
		return Reservation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase == PhaseFinalizing && !estimate.Finalization {
		return Reservation{}, ErrSoftLimit
	}
	estimate.Requests = max(estimate.Requests, 1)
	usage := estimate.usage()
	hardCost := m.limits.HardCostMicros
	if !estimate.Finalization {
		hardCost = max(int64(0), hardCost-m.limits.FinalizationReserveMicros)
	}
	projected := add(m.account.confirmed, m.account.uncertain, m.account.reserved, usage)
	if projected.CostMicros > hardCost {
		return Reservation{}, fmt.Errorf("%w: monetary cost", ErrHardLimit)
	}
	if exceeds(projected.InputTokens, m.limits.MaxInputTokens) || exceeds(projected.OutputTokens, m.limits.MaxOutputTokens) || exceeds(projected.ReasoningTokens, m.limits.MaxReasoningTokens) || exceeds(projected.CachedTokens, m.limits.MaxCachedTokens) || exceeds(projected.Requests, m.limits.MaxRequests) {
		return Reservation{}, fmt.Errorf("%w: token or request quota", ErrHardLimit)
	}
	if estimate.Provider != "" {
		providerAccount := m.providerAccount(estimate.Provider)
		providerProjected := add(providerAccount.confirmed, providerAccount.uncertain, providerAccount.reserved, usage)
		if limit, ok := m.limits.Providers[estimate.Provider]; ok {
			if providerProjected.CostMicros > limit.HardCostMicros || exceeds(providerProjected.Requests, limit.MaxRequests) {
				return Reservation{}, fmt.Errorf("%w: provider %s quota", ErrHardLimit, estimate.Provider)
			}
		}
	}
	m.nextID++
	reservation := Reservation{id: fmt.Sprintf("reservation-%d", m.nextID), manager: m, estimate: estimate}
	m.reservations[reservation.id] = estimate
	m.account.reserved = add(m.account.reserved, usage)
	if estimate.Provider != "" {
		providerAccount := m.providerAccount(estimate.Provider)
		providerAccount.reserved = add(providerAccount.reserved, usage)
	}
	if !estimate.Finalization && m.limits.SoftCostMicros > 0 && projected.CostMicros >= m.limits.SoftCostMicros {
		m.phase = PhaseFinalizing
	}
	if estimate.Finalization {
		m.phase = PhaseFinalizing
	}
	return reservation, nil
}

func (r Reservation) ID() string { return r.id }

func (r Reservation) Reconcile(usage Usage) error {
	if err := validUsage(usage); err != nil {
		return err
	}
	return r.manager.settle(r, &usage, false)
}

func (r Reservation) Release(reason error) error {
	if r.manager == nil {
		return ErrReservationClosed
	}
	return r.manager.settle(r, nil, reason != nil)
}

func (m *Manager) settle(reservation Reservation, actual *Usage, uncertain bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	storedEstimate, ok := m.reservations[reservation.id]
	if !ok {
		return ErrReservationClosed
	}
	delete(m.reservations, reservation.id)
	estimate := storedEstimate.usage()
	m.account.reserved = subtract(m.account.reserved, estimate)
	if reservation.estimate.Provider != "" {
		providerAccount := m.providerAccount(reservation.estimate.Provider)
		providerAccount.reserved = subtract(providerAccount.reserved, estimate)
	}
	if actual != nil {
		m.account.confirmed = add(m.account.confirmed, *actual)
		if reservation.estimate.Provider != "" {
			providerAccount := m.providerAccount(reservation.estimate.Provider)
			providerAccount.confirmed = add(providerAccount.confirmed, *actual)
		}
	} else if uncertain {
		m.account.uncertain = add(m.account.uncertain, estimate)
		if reservation.estimate.Provider != "" {
			providerAccount := m.providerAccount(reservation.estimate.Provider)
			providerAccount.uncertain = add(providerAccount.uncertain, estimate)
		}
	}
	if m.limits.SoftCostMicros > 0 && add(m.account.confirmed, m.account.uncertain, m.account.reserved).CostMicros >= m.limits.SoftCostMicros {
		m.phase = PhaseFinalizing
	}
	return nil
}

func (m *Manager) Finalize() {
	m.mu.Lock()
	m.phase = PhaseFinalizing
	m.mu.Unlock()
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := Status{
		Phase: m.phase, Confirmed: m.account.confirmed, Uncertain: m.account.uncertain, Reserved: m.account.reserved,
		EstimatedRemainingCostMicros: m.limits.HardCostMicros - add(m.account.confirmed, m.account.uncertain, m.account.reserved).CostMicros,
		Providers:                    map[string]AccountStatus{},
	}
	for name, account := range m.providers {
		status.Providers[name] = AccountStatus{Confirmed: account.confirmed, Uncertain: account.uncertain, Reserved: account.reserved}
	}
	return status
}

func (m *Manager) providerAccount(provider string) *account {
	if m.providers[provider] == nil {
		m.providers[provider] = &account{}
	}
	return m.providers[provider]
}

func (e Estimate) usage() Usage {
	return Usage{CostMicros: e.CostMicros, InputTokens: e.InputTokens, OutputTokens: e.OutputTokens, ReasoningTokens: e.ReasoningTokens, CachedTokens: e.CachedTokens, Requests: e.Requests}
}

func validUsage(usage Usage) error {
	values := []int64{usage.CostMicros, usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens, usage.CachedTokens, usage.Requests}
	for _, value := range values {
		if value < 0 {
			return errors.New("usage values must be non-negative")
		}
	}
	return nil
}

func add(values ...Usage) Usage {
	var result Usage
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

func subtract(left, right Usage) Usage {
	return Usage{
		CostMicros: left.CostMicros - right.CostMicros, InputTokens: left.InputTokens - right.InputTokens,
		OutputTokens: left.OutputTokens - right.OutputTokens, ReasoningTokens: left.ReasoningTokens - right.ReasoningTokens,
		CachedTokens: left.CachedTokens - right.CachedTokens, Requests: left.Requests - right.Requests,
	}
}

func exceeds(value, limit int64) bool { return limit > 0 && value > limit }
