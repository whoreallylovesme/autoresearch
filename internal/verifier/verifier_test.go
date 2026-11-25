package verifier_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
	"github.com/whoreallylovesme/autoresearch/internal/verifier"
)

func TestBenchmarkVerifierChecksMetricDirection(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "output"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "output", "metrics.json"), []byte(`{"accuracy": 0.92, "latency_ms": 15}`), 0o600); err != nil {
		t.Fatal(err)
	}
	accuracy := verifier.Benchmark{Metric: "accuracy", Direction: "min", Threshold: 0.9}
	result, err := accuracy.Verify(context.Background(), verifier.Request{ExperimentID: "experiment", Workspace: workspace})
	if err != nil || !result.Passed || result.ExitCode != 0 || result.Metrics["accuracy"] != 0.92 {
		t.Fatalf("Verify() = %#v, %v", result, err)
	}
	latency := verifier.Benchmark{Metric: "latency_ms", Direction: "max", Threshold: 10}
	result, err = latency.Verify(context.Background(), verifier.Request{ExperimentID: "experiment", Workspace: workspace})
	if err != nil || result.Passed || result.ExitCode == 0 {
		t.Fatalf("failed benchmark = %#v, %v", result, err)
	}
}

func TestBenchmarkVerifierRejectsMissingMetric(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "output"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "output", "metrics.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	benchmark := verifier.Benchmark{Metric: "missing", Direction: "min", Threshold: 1}
	if _, err := benchmark.Verify(context.Background(), verifier.Request{Workspace: workspace}); err == nil {
		t.Fatal("missing metric was accepted")
	}
}

func TestCustomCommandVerifierUsesExecutor(t *testing.T) {
	executor := &mockExecutor{resultExitCode: 0}
	check := verifier.CustomCommand{Executor: executor, Command: []string{"go", "test", "./..."}, Timeout: time.Minute}
	result, err := check.Verify(context.Background(), verifier.Request{ExperimentID: "experiment", Job: testJob()})
	if err != nil || !result.Passed || executor.job.Command[0] != "go" {
		t.Fatalf("Verify() = %#v, %v, job = %#v", result, err, executor.job)
	}
}

type mockExecutor struct {
	job            executor.Job
	resultExitCode int
}

func (m *mockExecutor) Submit(_ context.Context, job executor.Job) error { m.job = job; return nil }
func (m *mockExecutor) Status(context.Context, string) (executor.Result, error) {
	return executor.Result{}, nil
}
func (m *mockExecutor) Cancel(context.Context, string) error { return nil }
func (m *mockExecutor) Wait(context.Context, string) (executor.Result, error) {
	return executor.Result{State: executor.StateCompleted, ExitCode: m.resultExitCode}, nil
}
func (m *mockExecutor) Logs(context.Context, string) (<-chan executor.LogEntry, error) {
	channel := make(chan executor.LogEntry)
	close(channel)
	return channel, nil
}
func (m *mockExecutor) Collect(context.Context, string) ([]artifact.Record, error) { return nil, nil }

func testJob() executor.Job {
	return executor.Job{ID: "experiment", Command: []string{"python", "experiment.py"}, Timeout: time.Minute}
}
