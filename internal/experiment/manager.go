package experiment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/executor"
	"github.com/whoreallylovesme/autoresearch/internal/verifier"
)

type Manager struct {
	store    Store
	executor executor.Executor
	verifier verifier.Verifier
	mu       sync.Mutex
	runID    string
	jobs     map[string]executor.Job
	checks   map[string]chan struct{}
}

type Store interface {
	SaveExperiment(context.Context, Record) error
	LoadExperiment(context.Context, string, string) (Record, error)
	LoadExperiments(context.Context, string) ([]Record, error)
}

func New(store Store, runner executor.Executor, check verifier.Verifier) *Manager {
	return &Manager{store: store, executor: runner, verifier: check, jobs: map[string]executor.Job{}, checks: map[string]chan struct{}{}}
}

func (m *Manager) SetRunID(runID string) { m.runID = runID }

func (m *Manager) Submit(ctx context.Context, record Record, job executor.Job) error {
	if record.ID == "" || record.RunID == "" || m.executor == nil {
		return errors.New("experiment ID, run ID, and executor are required")
	}
	if _, err := m.store.LoadExperiment(ctx, record.RunID, record.ID); err == nil {
		return errors.New("experiment ID already exists")
	}
	m.runID = record.RunID
	job.ID = record.ID
	job.RunID = record.RunID
	record.Job = job
	record.Status = StatusCreated
	record.CreatedAt = time.Now().UTC()
	if err := m.store.SaveExperiment(ctx, record); err != nil {
		return err
	}
	if err := m.executor.Submit(ctx, job); err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		_ = m.store.SaveExperiment(ctx, record)
		return err
	}
	m.mu.Lock()
	m.jobs[record.ID] = job
	m.mu.Unlock()
	record.Status = StatusRunning
	record.StartedAt = time.Now().UTC()
	return m.store.SaveExperiment(ctx, record)
}

func (m *Manager) Wait(ctx context.Context, id string) (Record, error) {
	record, err := m.Get(ctx, id)
	if err != nil {
		return record, err
	}
	if record.Status != StatusRunning {
		return record, nil
	}
	result, err := m.executor.Wait(ctx, id)
	if err != nil {
		return record, err
	}
	record.Status = StatusCompleted
	record.Error = result.Error
	if result.State != executor.StateCompleted || result.ExitCode != 0 {
		record.Status = StatusFailed
	}
	record.EndedAt = time.Now().UTC()
	logs, err := m.executor.Logs(ctx, id)
	if err == nil {
		for entry := range logs {
			record.Logs = append(record.Logs, entry)
		}
	}
	artifacts, err := m.executor.Collect(ctx, id)
	if err != nil {
		return record, err
	}
	record.Artifacts = artifacts
	if content, readErr := os.ReadFile(filepath.Join(record.Job.Workspace, "output", "metrics.json")); readErr == nil {
		var metrics map[string]float64
		if err := json.Unmarshal(content, &metrics); err == nil {
			record.Metrics = metrics
		}
	}
	if err := m.store.SaveExperiment(ctx, record); err != nil {
		return record, err
	}
	return record, nil
}

func (m *Manager) Verify(ctx context.Context, id string) (Record, error) {
	m.mu.Lock()
	if done, exists := m.checks[id]; exists {
		m.mu.Unlock()
		select {
		case <-done:
			return m.Get(ctx, id)
		case <-ctx.Done():
			return Record{}, ctx.Err()
		}
	}
	done := make(chan struct{})
	m.checks[id] = done
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.checks, id)
		close(done)
		m.mu.Unlock()
	}()
	record, err := m.Wait(ctx, id)
	if err != nil || record.Status != StatusCompleted || m.verifier == nil {
		return record, err
	}
	verification, err := m.verifier.Verify(ctx, verifier.Request{ExperimentID: id, Workspace: record.Job.Workspace, Job: record.Job, Artifacts: record.Artifacts})
	if err != nil {
		record.Status = StatusFailed
		record.Error = err.Error()
		_ = m.store.SaveExperiment(ctx, record)
		return record, err
	}
	record.Verification = &verification
	if verification.Passed {
		record.Status = StatusVerified
	} else {
		record.Status = StatusRefuted
	}
	if err := m.store.SaveExperiment(ctx, record); err != nil {
		return record, err
	}
	return record, nil
}

func (m *Manager) Get(ctx context.Context, id string) (Record, error) {
	if m.runID == "" {
		return Record{}, errors.New("experiment run ID is not configured")
	}
	return m.store.LoadExperiment(ctx, m.runID, id)
}

func (m *Manager) List(ctx context.Context) ([]Record, error) {
	return m.store.LoadExperiments(ctx, m.runID)
}

func (m *Manager) Recover(ctx context.Context) error {
	records, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Status == StatusRunning {
			record.Status = StatusFailed
			record.Error = "experiment interrupted by runtime restart"
			if err := m.store.SaveExperiment(ctx, record); err != nil {
				return err
			}
		}
	}
	return nil
}
