package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
	"github.com/whoreallylovesme/autoresearch/internal/experiment"
	"github.com/whoreallylovesme/autoresearch/internal/graph"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
	"github.com/whoreallylovesme/autoresearch/internal/verifier"
)

type runtimeExecution struct {
	manager       *experiment.Manager
	definition    config.ExecutorDefinition
	workspaceRoot string
}

func (r *Runtime) attachExecution(store *storage.SQLite, artifacts *artifact.Store) error {
	definition, ok := r.configuration.Executors.Executors[r.configuration.Executors.Default]
	if !ok || definition.Type == "mock" {
		return nil
	}
	if definition.Type != "docker" {
		return fmt.Errorf("executor type %s is not implemented", definition.Type)
	}
	workspaceRoot := definition.WorkspaceRoot
	if strings.HasPrefix(workspaceRoot, "./data/") {
		workspaceRoot = filepath.Join(r.configuration.Runtime.DataDir, strings.TrimPrefix(workspaceRoot, "./data/"))
	} else if !filepath.IsAbs(workspaceRoot) {
		workspaceRoot = filepath.Join(r.configuration.BaseDir, workspaceRoot)
	}
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: workspaceRoot, AllowedImages: definition.AllowedImages, Artifacts: artifacts})
	if err != nil {
		return err
	}
	var check verifier.Verifier
	switch r.configuration.Task.Verification.Type {
	case "benchmark":
		check = verifier.Benchmark{Metric: r.configuration.Task.Verification.Metric, Direction: r.configuration.Task.Verification.Direction, Threshold: r.configuration.Task.Verification.Threshold}
	case "custom-command":
		check = verifier.CustomCommand{Executor: runner, Command: r.configuration.Task.Verification.Command, Timeout: r.configuration.Task.Verification.Timeout.Duration}
	default:
		return errors.New("unsupported experiment verifier")
	}
	manager := experiment.New(store, runner, check)
	manager.SetRunID(r.status.ID)
	r.execution = &runtimeExecution{manager: manager, definition: definition, workspaceRoot: workspaceRoot}
	return manager.Recover(context.Background())
}

func (r *Runtime) executeExecutionTool(ctx context.Context, agentID, name string, arguments json.RawMessage) (bool, json.RawMessage, error) {
	if r.execution == nil {
		return false, nil, nil
	}
	switch name {
	case "request_experiment":
		var input struct {
			ID             string   `json:"id"`
			HypothesisID   string   `json:"hypothesis_id"`
			Filename       string   `json:"filename"`
			Code           string   `json:"code"`
			Command        []string `json:"command"`
			Seed           int64    `json:"seed"`
			DatasetVersion string   `json:"dataset_version"`
			Dependencies   []string `json:"dependencies"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		if input.ID == "" || input.HypothesisID == "" || input.Filename == "" || input.Code == "" || len(input.Command) == 0 || filepath.Base(input.Filename) != input.Filename {
			return true, nil, errors.New("experiment ID, hypothesis, safe filename, code, and command are required")
		}
		if _, err := r.execution.manager.Get(ctx, input.ID); err == nil {
			return true, nil, errors.New("experiment already exists")
		}
		workspace := filepath.Join(r.execution.workspaceRoot, input.ID)
		if _, err := os.Stat(workspace); err == nil {
			return true, nil, errors.New("experiment workspace already exists")
		}
		if err := os.Mkdir(workspace, 0o700); err != nil {
			return true, nil, err
		}
		if err := os.WriteFile(filepath.Join(workspace, input.Filename), []byte(input.Code), 0o644); err != nil {
			return true, nil, err
		}
		digest := sha256.Sum256([]byte(input.Code))
		configuration, _ := json.Marshal(map[string]any{"seed": input.Seed, "filename": input.Filename, "command": input.Command})
		job := executor.Job{ID: input.ID, RunID: r.status.ID, Workspace: workspace, Image: r.execution.definition.Image, Command: input.Command, Env: map[string]string{"SEED": fmt.Sprintf("%d", input.Seed)}, Timeout: r.execution.definition.Timeout.Duration, MemoryMB: r.execution.definition.MaxMemoryMB, CPUs: r.execution.definition.MaxCPUs, PIDs: r.execution.definition.PIDs, Network: "none"}
		record := experiment.Record{ID: input.ID, RunID: r.status.ID, HypothesisID: input.HypothesisID, CodeSHA256: hex.EncodeToString(digest[:]), Configuration: configuration, DatasetVersion: input.DatasetVersion, Dependencies: input.Dependencies, RandomSeed: input.Seed, Hardware: map[string]string{"os": runtime.GOOS, "architecture": runtime.GOARCH}, ExecutorName: "docker"}
		if err := r.execution.manager.Submit(ctx, record, job); err != nil {
			return true, nil, err
		}
		node := graph.Node{ID: input.ID, RunID: r.status.ID, Type: graph.NodeExperiment, Author: agentID, Status: string(experiment.StatusRunning), Description: "Reproducible isolated experiment", Artifacts: nil}
		if err := r.science.graph.AddNode(node); err != nil {
			return true, nil, err
		}
		if err := r.science.graph.AddEdge(graph.Edge{ID: "investigates-" + input.ID, RunID: r.status.ID, FromNode: input.ID, ToNode: input.HypothesisID, Type: graph.EdgeInvestigates, Author: agentID}); err != nil {
			return true, nil, err
		}
		hypothesis, ok := r.science.graph.Node(input.HypothesisID)
		if ok && hypothesis.Status == graph.StatusProposed {
			if err := r.science.graph.TransitionHypothesis(input.HypothesisID, graph.StatusInvestigating, nil); err != nil {
				return true, nil, err
			}
		}
		r.mu.Lock()
		r.recordEventLocked("experiment.started", agentID, "", map[string]string{"experiment_id": input.ID})
		r.mu.Unlock()
		result, err := json.Marshal(map[string]string{"experiment_id": input.ID, "status": string(experiment.StatusRunning)})
		return true, result, err
	case "request_verification":
		var input struct {
			ExperimentID string `json:"experiment_id"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		record, err := r.execution.manager.Verify(ctx, input.ExperimentID)
		if err != nil {
			return true, nil, err
		}
		if err := r.recordExperimentVerification(agentID, record); err != nil {
			return true, nil, err
		}
		result, err := json.Marshal(record)
		return true, result, err
	case "inspect_artifact":
		var input struct {
			ExperimentID string `json:"experiment_id"`
		}
		if err := decode(arguments, &input); err != nil {
			return true, nil, err
		}
		record, err := r.execution.manager.Get(ctx, input.ExperimentID)
		if err != nil {
			return true, nil, err
		}
		result, err := json.Marshal(record)
		return true, result, err
	}
	return false, nil, nil
}

func (r *Runtime) recordExperimentVerification(agentID string, record experiment.Record) error {
	node, ok := r.science.graph.Node(record.ID)
	if !ok {
		node = graph.Node{ID: record.ID, RunID: r.status.ID, Type: graph.NodeExperiment, Author: agentID, Description: "Reproducible isolated experiment"}
	}
	node.Status = string(record.Status)
	node.Author = agentID
	node.Description = "Reproducible isolated experiment with durable verification"
	for _, record := range record.Artifacts {
		node.Artifacts = append(node.Artifacts, record.ID)
	}
	if err := r.science.graph.ReviseNode(node); err != nil {
		return err
	}
	if record.Verification == nil {
		return errors.New("experiment has no verification record")
	}
	evidence := graph.Node{ID: record.Verification.ID, RunID: r.status.ID, Type: graph.NodeEvidence, Author: record.Verification.Type, Status: fmt.Sprintf("passed=%t", record.Verification.Passed), Description: record.Verification.Details, Artifacts: record.Verification.ArtifactIDs}
	if err := r.science.graph.AddNode(evidence); err != nil {
		return err
	}
	if err := r.science.graph.AddEdge(graph.Edge{ID: "verifies-" + record.Verification.ID, RunID: r.status.ID, FromNode: record.Verification.ID, ToNode: record.ID, Type: graph.EdgeVerifies, Author: "verifier"}); err != nil {
		return err
	}
	hypothesis, ok := r.science.graph.Node(record.HypothesisID)
	if !ok {
		return errors.New("experiment hypothesis is missing")
	}
	if record.Status == experiment.StatusVerified {
		if hypothesis.Status == graph.StatusProposed {
			if err := r.science.graph.TransitionHypothesis(hypothesis.ID, graph.StatusInvestigating, nil); err != nil {
				return err
			}
			hypothesis.Status = graph.StatusInvestigating
		}
		if hypothesis.Status == graph.StatusInvestigating {
			if err := r.science.graph.TransitionHypothesis(hypothesis.ID, graph.StatusSupported, nil); err != nil {
				return err
			}
		}
		verification := graph.Verification{ID: record.Verification.ID, Verifier: record.Verification.Type, Passed: record.Verification.Passed, Command: strings.Join(record.Verification.Command, " "), ExitCode: record.Verification.ExitCode, ArtifactIDs: record.Verification.ArtifactIDs}
		if err := r.science.graph.TransitionHypothesis(hypothesis.ID, graph.StatusVerified, []graph.Verification{verification}); err != nil {
			return err
		}
	} else if hypothesis.Status != graph.StatusVerified {
		if err := r.science.graph.TransitionHypothesis(hypothesis.ID, graph.StatusRefuted, nil); err != nil {
			return err
		}
	}
	r.mu.Lock()
	r.recordEventLocked("experiment.completed", agentID, "", map[string]string{"experiment_id": record.ID, "status": string(record.Status), "verification_id": record.Verification.ID})
	r.mu.Unlock()
	return nil
}

func (r *Runtime) Experiments(ctx context.Context) ([]experiment.Record, error) {
	if r.execution == nil {
		return nil, nil
	}
	return r.execution.manager.List(ctx)
}
