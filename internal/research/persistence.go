package research

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/budget"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

type runtimePersistence struct {
	store     *storage.SQLite
	artifacts *artifact.Store
}

func NewPersistent(configuration *config.Config, factory ProviderFactory) (*Runtime, error) {
	if configuration == nil {
		return nil, errors.New("configuration is required")
	}
	store, err := storage.Open(filepath.Join(configuration.Runtime.DataDir, "autoresearch.db"))
	if err != nil {
		return nil, err
	}
	artifacts, err := artifact.New(filepath.Join(configuration.Runtime.DataDir, "artifacts"))
	if err != nil {
		store.Close()
		return nil, err
	}
	runtime, err := New(configuration, factory)
	if err != nil {
		store.Close()
		return nil, err
	}
	runtime.persistence = &runtimePersistence{store: store, artifacts: artifacts}
	if err := runtime.attachPersistentScience(store, artifacts); err != nil {
		store.Close()
		return nil, err
	}
	if err := runtime.attachExecution(store, artifacts); err != nil {
		store.Close()
		return nil, err
	}
	runtime.budget.SetPersistence(func(status budget.Status) error {
		return store.SaveBudget(context.Background(), runtime.status.ID, status)
	})
	snapshot, err := store.Load(context.Background(), runtime.status.ID)
	if err != nil {
		store.Close()
		return nil, err
	}
	if snapshot.Run == nil {
		if len(runtime.events) != 1 {
			store.Close()
			return nil, errors.New("new persistent runtime has invalid initial history")
		}
		event := runtime.storageEvent(runtime.events[0])
		if err := store.SaveRun(context.Background(), runtime.runRecord(), &event); err != nil {
			store.Close()
			return nil, err
		}
		if err := store.SaveBudget(context.Background(), runtime.status.ID, runtime.budget.Status()); err != nil {
			store.Close()
			return nil, err
		}
		if err := runtime.saveScienceGraph(); err != nil {
			store.Close()
			return nil, err
		}
		return runtime, nil
	}
	runtime.events = nil
	for _, stored := range snapshot.Events {
		runtime.events = append(runtime.events, Event{ID: stored.ID, RunID: stored.RunID, Sequence: stored.Sequence, Type: stored.Type, SchemaVersion: stored.SchemaVersion, Timestamp: stored.Timestamp, Actor: stored.Actor, TaskID: stored.TaskID, Payload: stored.Payload})
	}
	for _, stored := range snapshot.Findings {
		runtime.findings = append(runtime.findings, Finding{ID: stored.ID, TaskID: stored.TaskID, AgentID: stored.AgentID, Role: stored.Role, Kind: stored.Kind, Summary: stored.Summary, Evidence: stored.Evidence, CreatedAt: stored.CreatedAt})
	}
	runtime.messages = append(runtime.messages, snapshot.Messages...)
	runtime.nextFinding = uint64(len(snapshot.Findings))
	runtime.nextTask = uint64(len(snapshot.Tasks))
	if err := runtime.scheduler.RestoreTasks(snapshot.Tasks); err != nil {
		store.Close()
		return nil, err
	}
	runtime.status.State = State(snapshot.Run.State)
	runtime.status.StartedAt = snapshot.Run.StartedAt
	runtime.status.EndedAt = snapshot.Run.EndedAt
	runtime.report = snapshot.Run.Report
	if snapshot.Budget != nil {
		runtime.budget.RestoreStatus(*snapshot.Budget)
		if err := store.SaveBudget(context.Background(), runtime.status.ID, runtime.budget.Status()); err != nil {
			store.Close()
			return nil, err
		}
	}
	if (runtime.status.State == StateRunning || runtime.status.State == StateFinalizing) && configuration.Runtime.ResumeOnRestart {
		runtime.status.State = StateCreated
		runtime.status.EndedAt = time.Time{}
		runtime.recovered = true
		runtime.recordEventLocked("research.resumed", "runtime", "", map[string]int{"tasks": len(snapshot.Tasks), "findings": len(snapshot.Findings)})
	}
	return runtime, nil
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.workers.Wait()
	if r.persistence == nil {
		return nil
	}
	checkpointErr := r.persistence.store.Checkpoint(context.Background())
	return errors.Join(checkpointErr, r.persistence.store.Close())
}

func (r *Runtime) newEventLocked(eventType, actor, taskID string, payload any) Event {
	encoded, _ := json.Marshal(payload)
	if payload == nil {
		encoded = json.RawMessage(`{}`)
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		panic(err)
	}
	event := Event{ID: hex.EncodeToString(idBytes), RunID: r.status.ID, Sequence: uint64(len(r.events) + 1), Type: eventType, SchemaVersion: 1, Timestamp: time.Now().UTC(), Actor: actor, TaskID: taskID, Payload: encoded}
	r.events = append(r.events, event)
	return event
}

func (r *Runtime) persistRunEventLocked(event Event) {
	if r.persistence == nil {
		return
	}
	stored := r.storageEvent(event)
	if err := r.persistence.store.SaveRun(context.Background(), r.runRecord(), &stored); err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) persistTaskEventLocked(taskID, eventType, actor string, payload any) {
	event := r.newEventLocked(eventType, actor, taskID, payload)
	if r.persistence == nil {
		return
	}
	task, ok := r.scheduler.Get(taskID)
	if !ok {
		r.persistentErr = fmt.Errorf("persist task %s: not found", taskID)
		return
	}
	stored := r.storageEvent(event)
	if err := r.persistence.store.SaveTask(context.Background(), task, &stored); err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) completeTaskLocked(taskID, actor string, finding Finding) {
	message, err := messaging.New(r.status.ID, actor, "chief", messaging.TypeFinding, json.RawMessage(fmt.Sprintf(`{"summary":%q}`, finding.Summary)))
	if err == nil {
		message.TaskID = taskID
		r.messages = append(r.messages, message)
	}
	event := r.newEventLocked("task.completed", actor, taskID, nil)
	if r.persistence == nil {
		return
	}
	task, _ := r.scheduler.Get(taskID)
	storedFinding := storage.Finding{ID: finding.ID, RunID: r.status.ID, TaskID: finding.TaskID, AgentID: finding.AgentID, Role: finding.Role, Kind: finding.Kind, Summary: finding.Summary, Evidence: finding.Evidence, CreatedAt: finding.CreatedAt}
	storedEvent := r.storageEvent(event)
	if err := r.persistence.store.CompleteTask(context.Background(), task, storedFinding, &message, storedEvent); err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) persistFindingLocked(finding Finding) {
	event := r.newEventLocked("finding.submitted", finding.AgentID, finding.TaskID, nil)
	if r.persistence == nil {
		return
	}
	stored := storage.Finding{ID: finding.ID, RunID: r.status.ID, TaskID: finding.TaskID, AgentID: finding.AgentID, Role: finding.Role, Kind: finding.Kind, Summary: finding.Summary, Evidence: finding.Evidence, CreatedAt: finding.CreatedAt}
	storedEvent := r.storageEvent(event)
	if err := r.persistence.store.SaveFinding(context.Background(), stored, &storedEvent); err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) persistMessageLocked(message messaging.Message) {
	event := r.newEventLocked("agent.message", message.Sender, message.TaskID, message)
	if r.persistence == nil {
		return
	}
	stored := r.storageEvent(event)
	if err := r.persistence.store.SaveMessage(context.Background(), message, &stored); err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) persistArtifactLocked(name, mediaType string, content []byte) {
	if r.persistence == nil {
		return
	}
	record, err := r.persistence.artifacts.Put(context.Background(), r.status.ID, name, mediaType, content)
	if err == nil {
		err = r.persistence.store.SaveArtifact(context.Background(), record)
	}
	if err != nil {
		r.persistentErr = err
	}
}

func (r *Runtime) storageEvent(event Event) storage.Event {
	return storage.Event{ID: event.ID, RunID: event.RunID, Sequence: event.Sequence, Type: event.Type, SchemaVersion: event.SchemaVersion, Timestamp: event.Timestamp, Actor: event.Actor, TaskID: event.TaskID, Payload: event.Payload}
}

func (r *Runtime) runRecord() storage.Run {
	configuration, _ := json.Marshal(r.configuration.Snapshot)
	return storage.Run{ID: r.status.ID, Name: r.status.Name, State: string(r.status.State), StartedAt: r.status.StartedAt, EndedAt: r.status.EndedAt, Report: r.report, Configuration: configuration}
}

func (r *Runtime) maintenanceLoop(ctx context.Context) {
	defer r.workers.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastCheckpoint := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if recovered := r.scheduler.Recover(now.UTC()); recovered > 0 {
				for _, task := range r.scheduler.List() {
					if task.State == scheduler.StateRetryWait || task.State == scheduler.StateFailed {
						r.mu.Lock()
						r.persistTaskEventLocked(task.ID, "task.recovered", "runtime", nil)
						r.mu.Unlock()
					}
				}
			}
			r.mu.Lock()
			for _, current := range r.agents {
				current.status.LastHeartbeat = now.UTC()
			}
			r.mu.Unlock()
			if r.persistence != nil && now.Sub(lastCheckpoint) >= r.configuration.Runtime.CheckpointInterval.Duration {
				if err := r.persistence.store.Checkpoint(ctx); err != nil {
					r.mu.Lock()
					r.persistentErr = err
					r.mu.Unlock()
				}
				lastCheckpoint = now
			}
		}
	}
}

func (r *Runtime) executeWithHeartbeat(ctx context.Context, current *instance, lease *scheduler.Lease, assignment agentAssignment) (agentResult, error) {
	heartbeatDone := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	go func() {
		defer close(heartbeatStopped)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case now := <-ticker.C:
				if err := r.scheduler.Heartbeat(lease.ID, lease.FencingToken, now.Add(30*time.Second)); err != nil {
					return
				}
				r.mu.Lock()
				current.status.LastHeartbeat = now
				r.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	result, err := current.runner.Execute(ctx, assignment)
	close(heartbeatDone)
	<-heartbeatStopped
	return result, err
}
