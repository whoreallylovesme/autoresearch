package research

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/agent"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

type ProviderFactory func(agentID string, configuration config.AgentConfig) (provider.Provider, error)

type instance struct {
	configuration config.AgentConfig
	runner        *agent.Agent
	status        AgentStatus
}

type Runtime struct {
	mu            sync.Mutex
	configuration *config.Config
	scheduler     *scheduler.Scheduler
	budget        *budget.Manager
	agents        map[string]*instance
	status        Status
	findings      []Finding
	messages      []messaging.Message
	events        []Event
	report        string
	persistence   *runtimePersistence
	recovered     bool
	persistentErr error
	science       *runtimeScience
	execution     *runtimeExecution
	nextTask      uint64
	nextFinding   uint64
	workers       sync.WaitGroup
	cancel        context.CancelFunc
	resume        chan struct{}
}

func New(configuration *config.Config, factory ProviderFactory) (*Runtime, error) {
	if configuration == nil || factory == nil {
		return nil, errors.New("configuration and provider factory are required")
	}
	limits := budget.Limits{
		SoftCostMicros: micros(configuration.Budget.Money.SoftLimitUSD), HardCostMicros: micros(configuration.Budget.Money.HardLimitUSD),
		FinalizationReserveMicros: micros(configuration.Budget.Money.FinalizationReserveUSD), MaxRequests: 0,
		Providers: map[string]budget.ProviderLimits{},
	}
	for name, limit := range configuration.Budget.Providers {
		limits.Providers[name] = budget.ProviderLimits{HardCostMicros: micros(limit.MaxUSD), MaxRequests: limit.Quota.MaxRequests}
	}
	runtime := &Runtime{
		configuration: configuration, scheduler: scheduler.New(configuration.Runtime.MaxParallelTasks, 30*time.Second), budget: budget.NewManager(limits),
		agents: map[string]*instance{}, status: Status{ID: configuration.Name, Name: configuration.Name, Objective: configuration.Task.Objective, State: StateCreated}, resume: make(chan struct{}),
	}
	if err := runtime.addAgent("chief", configuration.Chief, factory); err != nil {
		return nil, err
	}
	for index, agentConfig := range configuration.Agents {
		if err := runtime.addAgent(fmt.Sprintf("worker-%d", index+1), agentConfig, factory); err != nil {
			return nil, err
		}
	}
	runtime.science = newRuntimeScience(runtime.status.ID, configuration.Task.Objective)
	runtime.recordEventLocked("research.created", "runtime", "", nil)
	return runtime, nil
}

func (r *Runtime) addAgent(id string, configuration config.AgentConfig, factory ProviderFactory) error {
	if configuration.Model.Provider != "mock" && configuration.Model.Pricing == nil {
		return fmt.Errorf("agent %s: real providers require pricing configuration", id)
	}
	model, err := factory(id, configuration)
	if err != nil {
		return fmt.Errorf("create provider for %s: %w", id, err)
	}
	current := &instance{configuration: configuration, status: AgentStatus{ID: id, Role: configuration.Role, Model: configuration.Model.Model, Provider: configuration.Model.Provider, State: "created", LastHeartbeat: time.Now().UTC(), LastActivity: time.Now().UTC()}}
	runtimeAgent := &accountedProvider{provider: model, manager: r.budget, model: configuration.Model, providerName: configuration.Model.Provider, finalizing: func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.status.State == StateFinalizing || r.status.State == StateCompleted || r.budget.Status().Phase == budget.PhaseFinalizing
	}, used: func(usage provider.Usage, _ int64) {
		r.mu.Lock()
		current.status.Usage.InputTokens += usage.InputTokens
		current.status.Usage.OutputTokens += usage.OutputTokens
		current.status.Usage.ReasoningTokens += usage.ReasoningTokens
		current.status.Usage.CachedTokens += usage.CachedTokens
		current.status.LastActivity = time.Now().UTC()
		current.status.LastHeartbeat = current.status.LastActivity
		r.mu.Unlock()
	}}
	current.runner = agent.New(id, configuration, runtimeAgent, r, 12)
	r.agents[id] = current
	return nil
}

func (r *Runtime) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.status.State != StateCreated {
		r.mu.Unlock()
		return errors.New("research run has already started")
	}
	r.status.State = StateRunning
	r.status.StartedAt = time.Now().UTC()
	r.recordEventLocked("research.started", "runtime", "", nil)
	chief := r.agents["chief"]
	for _, current := range r.agents {
		if current.configuration.Role != "chief-scientist" {
			current.status.State = "idle"
		}
	}
	r.mu.Unlock()
	runContext, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	defer cancel()
	for id, current := range r.agents {
		if current.configuration.Role == "chief-scientist" {
			continue
		}
		r.workers.Add(1)
		go r.workerLoop(runContext, id, current)
	}
	r.workers.Add(1)
	go r.maintenanceLoop(runContext)
	var err error
	for {
		_, err = chief.runner.Execute(runContext, agent.Assignment{RunID: r.status.ID, Objective: r.configuration.Task.Objective, Restrictions: r.configuration.Task.Restrictions})
		if err == nil && r.currentState() == StatePaused {
			if err = r.waitForResume(runContext); err == nil {
				continue
			}
		}
		break
	}
	cancel()
	r.workers.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil && r.persistentErr != nil {
		err = r.persistentErr
	}
	if err != nil {
		if ctx.Err() != nil {
			r.status.State = StateCancelled
			r.recordEventLocked("research.cancelled", "runtime", "", nil)
			return ctx.Err()
		}
		r.status.State = StateFailed
		r.status.EndedAt = time.Now().UTC()
		r.recordEventLocked("research.failed", "runtime", "", map[string]string{"error": err.Error()})
		return err
	}
	if r.status.State != StateCompleted {
		r.status.State = StateFailed
		r.status.EndedAt = time.Now().UTC()
		r.recordEventLocked("research.failed", "runtime", "", map[string]string{"error": "chief finished without finalize_research"})
		return errors.New("chief finished without finalize_research")
	}
	return nil
}

func (r *Runtime) workerLoop(ctx context.Context, id string, current *instance) {
	defer r.workers.Done()
	for {
		if r.isPaused() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				continue
			}
		}
		lease, err := r.scheduler.Lease(ctx, scheduler.Worker{ID: id, Roles: []string{current.configuration.Role}})
		if errors.Is(err, scheduler.ErrNoReadyTask) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
				continue
			}
		}
		if err != nil {
			return
		}
		r.mu.Lock()
		current.status.State = "running"
		current.status.CurrentTask = lease.Task.ID
		current.status.LastHeartbeat = time.Now().UTC()
		r.persistTaskEventLocked(lease.Task.ID, "task.started", id, nil)
		r.mu.Unlock()
		result, executeErr := r.executeWithHeartbeat(ctx, current, lease, agent.Assignment{RunID: r.status.ID, TaskID: lease.Task.ID, Objective: lease.Task.Description, Restrictions: r.configuration.Task.Restrictions})
		r.mu.Lock()
		if executeErr != nil {
			if ctx.Err() == nil {
				_ = r.scheduler.Fail(lease.ID, lease.FencingToken, executeErr, true)
				current.status.LastError = executeErr.Error()
				current.status.State = "retrying"
				r.persistTaskEventLocked(lease.Task.ID, "task.failed", id, map[string]string{"error": executeErr.Error()})
			}
		} else {
			if err := r.scheduler.Complete(lease.ID, lease.FencingToken, scheduler.Result{Output: result.Content}); err == nil {
				kind := "finding"
				if current.configuration.Role == "critic" {
					kind = "criticism"
				} else if current.configuration.Role == "brainstormer" {
					kind = "speculation"
				}
				finding := r.addFindingLocked(id, current.configuration.Role, lease.Task.ID, kind, result.Content, nil)
				current.status.State = "idle"
				current.status.LastMeaningfulWork = time.Now().UTC()
				r.completeTaskLocked(lease.Task.ID, id, finding)
			}
		}
		current.status.CurrentTask = ""
		current.status.LastHeartbeat = time.Now().UTC()
		r.mu.Unlock()
	}
}

func (r *Runtime) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	status := r.status
	status.Budget = r.budget.Status()
	return status
}

func (r *Runtime) Tasks() []scheduler.Task { return r.scheduler.List() }

func (r *Runtime) Findings() []Finding {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Finding(nil), r.findings...)
}

func (r *Runtime) Agents() []AgentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	agents := make([]AgentStatus, 0, len(r.agents))
	for _, current := range r.agents {
		agents = append(agents, current.status)
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	return agents
}

func (r *Runtime) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func (r *Runtime) Messages() []messaging.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]messaging.Message(nil), r.messages...)
}

func (r *Runtime) Report() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.report
}

func (r *Runtime) recordEventLocked(eventType, actor, taskID string, payload any) {
	event := r.newEventLocked(eventType, actor, taskID, payload)
	r.persistRunEventLocked(event)
}

func (r *Runtime) addFindingLocked(agentID, role, taskID, kind, summary string, evidence []string) Finding {
	r.nextFinding++
	finding := Finding{ID: fmt.Sprintf("finding-%d", r.nextFinding), TaskID: taskID, AgentID: agentID, Role: role, Kind: kind, Summary: summary, Evidence: evidence, CreatedAt: time.Now().UTC()}
	r.findings = append(r.findings, finding)
	return finding
}

func micros(usd float64) int64 { return int64(usd * 1e6) }
