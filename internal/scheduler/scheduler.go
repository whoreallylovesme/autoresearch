package scheduler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"
)

var (
	ErrNoReadyTask   = errors.New("no ready task")
	ErrDuplicateTask = errors.New("duplicate task idempotency key")
	ErrStaleLease    = errors.New("stale or inactive lease")
	ErrTaskNotFound  = errors.New("task not found")
)

type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateRetryWait State = "retry_wait"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

type Task struct {
	ID             string            `json:"id"`
	RunID          string            `json:"run_id"`
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	Role           string            `json:"role"`
	Priority       int               `json:"priority"`
	Dependencies   []string          `json:"dependencies,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	MaxAttempts    int               `json:"max_attempts"`
	RetryBackoff   time.Duration     `json:"retry_backoff"`
	CreatedAt      time.Time         `json:"created_at"`
	Deadline       time.Time         `json:"deadline,omitempty"`
	NextAttemptAt  time.Time         `json:"next_attempt_at"`
	Attempts       int               `json:"attempts"`
	State          State             `json:"state"`
	Result         Result            `json:"result"`
	LastError      string            `json:"last_error,omitempty"`
	AssignedAgent  string            `json:"assigned_agent,omitempty"`
	LeaseID        string            `json:"lease_id,omitempty"`
	FencingToken   uint64            `json:"fencing_token,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Result struct {
	Output    string            `json:"output"`
	Artifacts []string          `json:"artifacts,omitempty"`
	Metrics   map[string]string `json:"metrics,omitempty"`
}

type Worker struct {
	ID    string
	Roles []string
}

type Lease struct {
	ID           string
	Task         Task
	Owner        string
	FencingToken uint64
	ExpiresAt    time.Time
}

type leaseRecord struct {
	lease     Lease
	active    bool
	completed bool
}

type Scheduler struct {
	mu            sync.Mutex
	maxParallel   int
	leaseDuration time.Duration
	tasks         map[string]*Task
	keys          map[string]string
	leases        map[string]*leaseRecord
	active        int
	nextLease     uint64
	nextFence     uint64
}

func New(maxParallel int, leaseDuration time.Duration) *Scheduler {
	return &Scheduler{maxParallel: maxParallel, leaseDuration: leaseDuration, tasks: map[string]*Task{}, keys: map[string]string{}, leases: map[string]*leaseRecord{}}
}

func (s *Scheduler) RestoreTasks(tasks []Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, task := range tasks {
		if task.ID == "" || task.Role == "" {
			return errors.New("restored task ID and role are required")
		}
		if _, exists := s.tasks[task.ID]; exists {
			return ErrDuplicateTask
		}
		if task.MaxAttempts < 1 {
			task.MaxAttempts = 1
		}
		if task.State == StateRunning {
			task.State = StatePending
			task.MaxAttempts = max(task.MaxAttempts, task.Attempts+1)
			task.LastError = "recovered interrupted lease"
			task.LeaseID = ""
			task.AssignedAgent = ""
		}
		if task.State == StatePending && task.NextAttemptAt.IsZero() {
			task.NextAttemptAt = now
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = now
		}
		s.nextFence = max(s.nextFence, task.FencingToken)
		task.FencingToken = 0
		copy := task
		s.tasks[task.ID] = &copy
		if task.IdempotencyKey != "" {
			s.keys[task.IdempotencyKey] = task.ID
		}
	}
	s.nextLease = uint64(len(tasks))
	s.nextFence++
	return nil
}

func (s *Scheduler) Submit(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if task.ID == "" || task.Role == "" {
		return errors.New("task ID and role are required")
	}
	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task ID %s: %w", task.ID, ErrDuplicateTask)
	}
	if task.IdempotencyKey != "" {
		if _, exists := s.keys[task.IdempotencyKey]; exists {
			return ErrDuplicateTask
		}
	}
	for _, dependency := range task.Dependencies {
		if dependency == task.ID {
			return errors.New("task cannot depend on itself")
		}
		if _, exists := s.tasks[dependency]; !exists {
			return fmt.Errorf("dependency %s does not exist", dependency)
		}
	}
	if task.MaxAttempts == 0 {
		task.MaxAttempts = 1
	}
	if task.MaxAttempts < 1 {
		return errors.New("max attempts must be positive")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	task.State = StatePending
	task.NextAttemptAt = task.CreatedAt
	copy := task
	s.tasks[task.ID] = &copy
	if task.IdempotencyKey != "" {
		s.keys[task.IdempotencyKey] = task.ID
	}
	if s.hasCycle(task.ID) {
		delete(s.tasks, task.ID)
		if task.IdempotencyKey != "" {
			delete(s.keys, task.IdempotencyKey)
		}
		return errors.New("task dependency cycle detected")
	}
	return nil
}

func (s *Scheduler) Lease(ctx context.Context, worker Worker) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active >= s.maxParallel {
		return nil, ErrNoReadyTask
	}
	candidates := make([]*Task, 0)
	now := time.Now().UTC()
	for _, task := range s.tasks {
		if (task.State == StatePending || task.State == StateRetryWait) && !task.NextAttemptAt.After(now) && s.dependenciesComplete(task) && (len(worker.Roles) == 0 || slices.Contains(worker.Roles, task.Role)) {
			if !task.Deadline.IsZero() && !task.Deadline.After(now) {
				task.State = StateFailed
				task.LastError = "deadline exceeded before dispatch"
				continue
			}
			candidates = append(candidates, task)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrNoReadyTask
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		if !candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].ID < candidates[j].ID
	})
	task := candidates[0]
	s.nextLease++
	s.nextFence++
	lease := Lease{ID: fmt.Sprintf("lease-%d", s.nextLease), Task: *task, Owner: worker.ID, FencingToken: s.nextFence, ExpiresAt: now.Add(s.leaseDuration)}
	task.State = StateRunning
	task.Attempts++
	task.AssignedAgent = worker.ID
	task.LeaseID = lease.ID
	task.FencingToken = lease.FencingToken
	lease.Task = *task
	s.leases[lease.ID] = &leaseRecord{lease: lease, active: true}
	s.active++
	result := lease
	return &result, nil
}

func (s *Scheduler) Heartbeat(leaseID string, fencingToken uint64, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, _, err := s.activeLease(leaseID, fencingToken)
	if err != nil {
		return err
	}
	if expiresAt.Before(time.Now().UTC()) {
		return errors.New("heartbeat expiry must be in the future")
	}
	record.lease.ExpiresAt = expiresAt
	return nil
}

func (s *Scheduler) Complete(leaseID string, fencingToken uint64, result Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, task, err := s.activeLease(leaseID, fencingToken)
	if err != nil {
		if record != nil && record.completed && record.lease.FencingToken == fencingToken {
			return nil
		}
		return err
	}
	task.State = StateCompleted
	task.Result = result
	task.LastError = ""
	record.active = false
	record.completed = true
	s.active--
	return nil
}

func (s *Scheduler) Fail(leaseID string, fencingToken uint64, cause error, retryable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, task, err := s.activeLease(leaseID, fencingToken)
	if err != nil {
		return err
	}
	if cause == nil {
		cause = errors.New("task failed")
	}
	task.LastError = cause.Error()
	if retryable && task.Attempts < task.MaxAttempts {
		task.State = StateRetryWait
		task.NextAttemptAt = time.Now().UTC().Add(task.RetryBackoff)
	} else {
		task.State = StateFailed
	}
	record.active = false
	s.active--
	return nil
}

func (s *Scheduler) Recover(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := 0
	for _, record := range s.leases {
		if !record.active || record.lease.ExpiresAt.After(now) {
			continue
		}
		task := s.tasks[record.lease.Task.ID]
		record.active = false
		s.active--
		recovered++
		if task.Attempts < task.MaxAttempts {
			task.State = StateRetryWait
			task.NextAttemptAt = now.Add(task.RetryBackoff)
			task.LastError = "lease expired"
		} else {
			task.State = StateFailed
			task.LastError = "lease expired after final attempt"
		}
	}
	return recovered
}

func (s *Scheduler) Reprioritize(taskID string, priority int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	task.Priority = priority
	return nil
}

func (s *Scheduler) Cancel(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if task.State == StateCompleted || task.State == StateFailed || task.State == StateCancelled {
		return nil
	}
	if record, ok := s.leases[task.LeaseID]; ok && record.active {
		record.active = false
		s.active--
	}
	task.State = StateCancelled
	task.LastError = "cancelled"
	return nil
}

func (s *Scheduler) Get(taskID string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return Task{}, false
	}
	return *task, true
}

func (s *Scheduler) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, *task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks
}

func (s *Scheduler) activeLease(leaseID string, fencingToken uint64) (*leaseRecord, *Task, error) {
	record, ok := s.leases[leaseID]
	if !ok || record.lease.FencingToken != fencingToken || !record.active {
		return record, nil, ErrStaleLease
	}
	task := s.tasks[record.lease.Task.ID]
	if task.State != StateRunning || task.LeaseID != leaseID || task.FencingToken != fencingToken {
		return record, task, ErrStaleLease
	}
	return record, task, nil
}

func (s *Scheduler) dependenciesComplete(task *Task) bool {
	for _, dependency := range task.Dependencies {
		if s.tasks[dependency].State != StateCompleted {
			return false
		}
	}
	return true
}

func (s *Scheduler) hasCycle(start string) bool {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, dependency := range s.tasks[id].Dependencies {
			if visit(dependency) {
				return true
			}
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	return visit(start)
}
