package research

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/whoreallylovesme/autoresearch/internal/experiment"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

type PlaybackFrame struct {
	Until         uint64              `json:"until"`
	ResearchState State               `json:"research_state"`
	Tasks         []scheduler.Task    `json:"tasks"`
	Graph         graph.Snapshot      `json:"graph"`
	Experiments   []experiment.Record `json:"experiments"`
	Events        []Event             `json:"events"`
	Report        string              `json:"report"`
}

func (r *Runtime) Playback(until uint64) (PlaybackFrame, error) {
	return r.PlaybackContext(context.Background(), until)
}

func (r *Runtime) PlaybackContext(ctx context.Context, until uint64) (PlaybackFrame, error) {
	r.mu.Lock()
	events := append([]Event(nil), r.events...)
	currentTasks := map[string]scheduler.Task{}
	for _, task := range r.scheduler.List() {
		currentTasks[task.ID] = task
	}
	currentNodes := map[string]graph.Node{}
	for _, node := range r.science.graph.Nodes() {
		currentNodes[node.ID] = node
	}
	var currentExperiments []experiment.Record
	if r.execution != nil {
		currentExperiments, _ = r.execution.manager.List(ctx)
	}
	finalReport := r.report
	r.mu.Unlock()
	if until == 0 || until > uint64(len(events)) {
		until = uint64(len(events))
	}
	frame := PlaybackFrame{Until: until, ResearchState: StateCreated, Events: append([]Event(nil), events[:until]...)}
	experimentByID := map[string]experiment.Record{}
	for _, record := range currentExperiments {
		experimentByID[record.ID] = record
	}
	researchGraph := graph.New()
	tasks := map[string]scheduler.Task{}
	experiments := map[string]experiment.Record{}
	for _, event := range events[:until] {
		switch event.Type {
		case "research.created":
			frame.ResearchState = StateCreated
			if objective, ok := currentNodes["objective-"+event.RunID]; ok {
				objective.CreatedAt = event.Timestamp
				_ = researchGraph.AddNode(objective)
			}
		case "research.started", "research.resumed":
			frame.ResearchState = StateRunning
		case "research.paused":
			frame.ResearchState = StatePaused
		case "research.finalizing":
			frame.ResearchState = StateFinalizing
		case "research.completed":
			frame.ResearchState = StateCompleted
			frame.Report = finalReport
			if conclusion, ok := currentNodes["conclusion-final"]; ok {
				conclusion.CreatedAt = event.Timestamp
				_ = researchGraph.AddNode(conclusion)
			}
		case "research.failed":
			frame.ResearchState = StateFailed
		case "research.cancelled":
			frame.ResearchState = StateCancelled
		case "task.created":
			task := currentTasks[event.TaskID]
			task.State = scheduler.StatePending
			task.CreatedAt = event.Timestamp
			task.Attempts = 0
			task.AssignedAgent = ""
			task.Result = scheduler.Result{}
			tasks[event.TaskID] = task
		case "task.started":
			task := tasks[event.TaskID]
			task.State = scheduler.StateRunning
			task.AssignedAgent = event.Actor
			task.Attempts++
			tasks[event.TaskID] = task
		case "task.completed":
			task := tasks[event.TaskID]
			task.State = scheduler.StateCompleted
			task.AssignedAgent = event.Actor
			task.Result = currentTasks[event.TaskID].Result
			tasks[event.TaskID] = task
		case "task.failed":
			task := tasks[event.TaskID]
			task.State = scheduler.StateFailed
			tasks[event.TaskID] = task
		case "hypothesis.proposed":
			hypothesis := currentNodes[event.PayloadID("id")]
			if err := json.Unmarshal(event.Payload, &hypothesis); err == nil {
			}
			hypothesis.CreatedAt = event.Timestamp
			hypothesis.Status = graph.StatusProposed
			_ = researchGraph.AddNode(hypothesis)
		case "experiment.started":
			var payload map[string]string
			_ = json.Unmarshal(event.Payload, &payload)
			id := payload["experiment_id"]
			record := experimentByID[id]
			record.Status = experiment.StatusRunning
			record.StartedAt = event.Timestamp
			experiments[id] = record
			if node, ok := currentNodes[id]; ok {
				node.Status = string(experiment.StatusRunning)
				node.CreatedAt = event.Timestamp
				_ = researchGraph.AddNode(node)
				if hypothesis, exists := currentNodes[record.HypothesisID]; exists {
					hypothesis.CreatedAt = event.Timestamp
					_ = researchGraph.AddNode(hypothesis)
					_ = researchGraph.AddEdge(graph.Edge{ID: "investigates-" + id, RunID: event.RunID, FromNode: id, ToNode: record.HypothesisID, Type: graph.EdgeInvestigates, Author: event.Actor, CreatedAt: event.Timestamp})
				}
			}
		case "experiment.completed":
			var payload map[string]string
			_ = json.Unmarshal(event.Payload, &payload)
			id := payload["experiment_id"]
			record := experiments[id]
			record.Status = experiment.Status(payload["status"])
			record.Verification = experimentByID[id].Verification
			experiments[id] = record
			if node, ok := currentNodes[id]; ok {
				node.Status = string(record.Status)
				if err := researchGraph.ReviseNode(node); err != nil {
					_ = researchGraph.AddNode(node)
				}
			}
			if evidenceID := payload["verification_id"]; evidenceID != "" {
				verification := experimentByID[id].Verification
				if verification != nil {
					evidence := currentNodes[evidenceID]
					evidence.CreatedAt = event.Timestamp
					_ = researchGraph.AddNode(evidence)
					_ = researchGraph.AddEdge(graph.Edge{ID: "verifies-" + evidenceID, RunID: event.RunID, FromNode: evidenceID, ToNode: id, Type: graph.EdgeVerifies, Author: "verifier", CreatedAt: event.Timestamp})
				}
			}
			if record.Status == experiment.StatusVerified {
				if hypothesis, ok := currentNodes[record.HypothesisID]; ok {
					hypothesis.Status = graph.StatusVerified
					if err := researchGraph.ReviseNode(hypothesis); err != nil {
						_ = researchGraph.AddNode(hypothesis)
					}
				}
			}
		}
	}
	for _, task := range tasks {
		frame.Tasks = append(frame.Tasks, task)
	}
	sort.Slice(frame.Tasks, func(i, j int) bool { return frame.Tasks[i].ID < frame.Tasks[j].ID })
	for _, record := range experiments {
		frame.Experiments = append(frame.Experiments, record)
	}
	sort.Slice(frame.Experiments, func(i, j int) bool { return frame.Experiments[i].ID < frame.Experiments[j].ID })
	frame.Graph = researchGraph.Snapshot()
	if err := ctx.Err(); err != nil {
		return PlaybackFrame{}, errors.New("playback cancelled: " + err.Error())
	}
	return frame, nil
}

func (e Event) PayloadID(key string) string {
	var payload map[string]string
	_ = json.Unmarshal(e.Payload, &payload)
	return payload[key]
}
