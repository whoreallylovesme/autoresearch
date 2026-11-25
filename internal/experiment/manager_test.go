package experiment_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
	"github.com/whoreallylovesme/autoresearch/internal/experiment"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
	"github.com/whoreallylovesme/autoresearch/internal/verifier"
)

const testImage = "python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b"

func TestExperimentLifecyclePersistsVerifiedReproducibilityRecord(t *testing.T) {
	requireDocker(t)
	sqlite, runner, _, workspace := setupExperiment(t, "verified")
	defer sqlite.Close()
	store := experiment.New(sqlite, runner, verifier.Benchmark{Metric: "answer", Direction: "min", Threshold: 6})
	record := experiment.Record{ID: "experiment-1", RunID: "run", HypothesisID: "hypothesis-1", CodeSHA256: "code-hash", Configuration: []byte(`{"case":"sum"}`), RandomSeed: 42, ExecutorName: "docker"}
	job := executor.Job{ID: record.ID, RunID: record.RunID, Workspace: workspace, Image: testImage, Command: []string{"python", "/work/experiment.py"}, Env: map[string]string{"SEED": "42"}, Timeout: 30 * time.Second, MemoryMB: 128, CPUs: 0.5, PIDs: 64}
	if err := store.Submit(context.Background(), record, job); err != nil {
		t.Fatal(err)
	}
	verified, err := store.Verify(context.Background(), record.ID)
	if err != nil || verified.Status != experiment.StatusVerified || verified.Verification == nil || !verified.Verification.Passed {
		t.Fatalf("Verify() = %#v, %v", verified, err)
	}
	if verified.Metrics["answer"] != 6 || len(verified.Artifacts) != 1 || len(verified.Logs) == 0 {
		t.Fatalf("record = %#v", verified)
	}
	loaded, err := store.Get(context.Background(), record.ID)
	if err != nil || loaded.Status != experiment.StatusVerified || loaded.CodeSHA256 != "code-hash" || loaded.RandomSeed != 42 {
		t.Fatalf("Get() = %#v, %v", loaded, err)
	}
}

func TestExperimentPersistsRefutedResult(t *testing.T) {
	requireDocker(t)
	sqlite, runner, _, workspace := setupExperiment(t, "refuted")
	defer sqlite.Close()
	store := experiment.New(sqlite, runner, verifier.Benchmark{Metric: "answer", Direction: "min", Threshold: 7})
	record := experiment.Record{ID: "experiment-2", RunID: "run", HypothesisID: "hypothesis-1", ExecutorName: "docker"}
	job := executor.Job{ID: record.ID, RunID: record.RunID, Workspace: workspace, Image: testImage, Command: []string{"python", "/work/experiment.py"}, Timeout: 30 * time.Second, MemoryMB: 128, CPUs: 0.5, PIDs: 64}
	if err := store.Submit(context.Background(), record, job); err != nil {
		t.Fatal(err)
	}
	refuted, err := store.Verify(context.Background(), record.ID)
	if err != nil || refuted.Status != experiment.StatusRefuted || refuted.Verification.Passed {
		t.Fatalf("Verify() = %#v, %v", refuted, err)
	}
}

func setupExperiment(t *testing.T, id string) (*storage.SQLite, *executor.DockerExecutor, *artifact.Store, string) {
	t.Helper()
	root := t.TempDir()
	sqlite, err := storage.Open(filepath.Join(root, "research.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SaveRun(context.Background(), storage.Run{ID: "run", Name: "run", State: "running"}, nil); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.New(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceRoot := filepath.Join(root, "workspaces")
	workspace := filepath.Join(workspaceRoot, id)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	source := "import json, pathlib\npathlib.Path('output').mkdir(exist_ok=True)\npathlib.Path('output/metrics.json').write_text(json.dumps({'answer': sum([1,2,3])}))\nprint('metric recorded')\n"
	if err := os.WriteFile(filepath.Join(workspace, "experiment.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: workspaceRoot, AllowedImages: []string{testImage}, Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	return sqlite, runner, artifacts, workspace
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skip("Docker daemon unavailable")
	}
}
