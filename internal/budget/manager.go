package budget

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	Provider        string `json:"provider,omitempty"`
	CostMicros      int64  `json:"cost_micros"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	ReasoningTokens int64  `json:"reasoning_tokens"`
	CachedTokens    int64  `json:"cached_tokens"`
	Requests        int64  `json:"requests"`
	Finalization    bool   `json:"finalization"`
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
	Phase                         Phase                    `json:"phase"`
	Confirmed                     Usage                    `json:"confirmed"`
	Uncertain                     Usage                    `json:"uncertain"`
	Reserved                      Usage                    `json:"reserved"`
	EstimatedRemainingCostMicros  int64                    `json:"estimated_remaining_cost_micros"`
	Providers                     map[string]AccountStatus `json:"providers,omitempty"`
	OpenReservations              []ReservationStatus      `json:"open_reservations,omitempty"`
	SoftLimitCostMicros           int64                    `json:"soft_limit_cost_micros"`
	HardLimitCostMicros           int64                    `json:"hard_limit_cost_micros"`
	FinalizationReserveCostMicros int64                    `json:"finalization_reserve_cost_micros"`
}

type ReservationStatus struct {
	ID       string   `json:"id"`
	Estimate Estimate `json:"estimate"`
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
	persistence  func(Status) error
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
	previousPhase := m.phase
	previousNextID := m.nextID
	previousAccount := m.account
	previousProviders := cloneAccounts(m.providers)
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
	m.applyAccountMutation(estimate.Provider, func(account *account) {
		account.reserved = add(account.reserved, usage)
	})
	if !estimate.Finalization && m.limits.SoftCostMicros > 0 && projected.CostMicros >= m.limits.SoftCostMicros {
		m.phase = PhaseFinalizing
	}
	if estimate.Finalization {
		m.phase = PhaseFinalizing
	}
	if err := m.persistLocked(); err != nil {
		delete(m.reservations, reservation.id)
		m.phase = previousPhase
		m.nextID = previousNextID
		m.account = previousAccount
		m.providers = previousProviders
		return Reservation{}, err
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
	previousPhase := m.phase
	previousAccount := m.account
	previousProviders := cloneAccounts(m.providers)
	delete(m.reservations, reservation.id)
	estimate := storedEstimate.usage()
	m.applyAccountMutation(reservation.estimate.Provider, func(account *account) {
		account.reserved = subtract(account.reserved, estimate)
	})
	if actual != nil {
		m.applyAccountMutation(reservation.estimate.Provider, func(account *account) {
			account.confirmed = add(account.confirmed, *actual)
		})
	} else if uncertain {
		m.applyAccountMutation(reservation.estimate.Provider, func(account *account) {
			account.uncertain = add(account.uncertain, estimate)
		})
	}
	if m.limits.SoftCostMicros > 0 && add(m.account.confirmed, m.account.uncertain, m.account.reserved).CostMicros >= m.limits.SoftCostMicros {
		m.phase = PhaseFinalizing
	}
	if err := m.persistLocked(); err != nil {
		m.reservations[reservation.id] = storedEstimate
		m.phase = previousPhase
		m.account = previousAccount
		m.providers = previousProviders
		return err
	}
	return nil
}

func (m *Manager) Finalize() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phase = PhaseFinalizing
	return m.persistLocked()
}

func (m *Manager) SetPersistence(persistence func(Status) error) {
	m.mu.Lock()
	m.persistence = persistence
	m.mu.Unlock()
}

func (m *Manager) RestoreStatus(status Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.phase = status.Phase
	m.account = account{confirmed: status.Confirmed, uncertain: status.Uncertain, reserved: status.Reserved}
	m.providers = map[string]*account{}
	for name, restored := range status.Providers {
		m.providers[name] = &account{confirmed: restored.Confirmed, uncertain: restored.Uncertain, reserved: restored.Reserved}
	}
	missingProviderReserved := map[string]Usage{}
	for _, reservation := range status.OpenReservations {
		provider := reservation.Estimate.Provider
		if provider == "" {
			continue
		}
		if _, ok := m.providers[provider]; !ok {
			missingProviderReserved[provider] = add(missingProviderReserved[provider], reservation.Estimate.usage())
		}
	}
	for provider, reserved := range missingProviderReserved {
		m.providers[provider] = &account{reserved: reserved}
	}
	for _, reservation := range status.OpenReservations {
		estimate := reservation.Estimate.usage()
		m.applyAccountMutation(reservation.Estimate.Provider, func(account *account) {
			account.reserved = subtract(account.reserved, estimate)
			account.uncertain = add(account.uncertain, estimate)
		})
		m.nextID++
	}
	m.reservations = map[string]Estimate{}
}

func (m *Manager) persistLocked() error {
	if m.persistence == nil {
		return nil
	}
	return m.persistence(m.statusLocked())
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	status := Status{
		SoftLimitCostMicros: m.limits.SoftCostMicros, HardLimitCostMicros: m.limits.HardCostMicros, FinalizationReserveCostMicros: m.limits.FinalizationReserveMicros,
		Phase: m.phase, Confirmed: m.account.confirmed, Uncertain: m.account.uncertain, Reserved: m.account.reserved,
		EstimatedRemainingCostMicros: m.limits.HardCostMicros - add(m.account.confirmed, m.account.uncertain, m.account.reserved).CostMicros,
		Providers:                    map[string]AccountStatus{},
	}
	for name, account := range m.providers {
		status.Providers[name] = AccountStatus{Confirmed: account.confirmed, Uncertain: account.uncertain, Reserved: account.reserved}
	}
	for id, estimate := range m.reservations {
		status.OpenReservations = append(status.OpenReservations, ReservationStatus{ID: id, Estimate: estimate})
	}
	sort.Slice(status.OpenReservations, func(i, j int) bool { return status.OpenReservations[i].ID < status.OpenReservations[j].ID })
	return status
}

func (m *Manager) applyAccountMutation(provider string, mutation func(*account)) {
	mutation(&m.account)
	if provider != "" {
		mutation(m.providerAccount(provider))
	}
}

func cloneAccounts(accounts map[string]*account) map[string]*account {
	cloned := make(map[string]*account, len(accounts))
	for name, account := range accounts {
		clonedAccount := *account
		cloned[name] = &clonedAccount
	}
	return cloned
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
