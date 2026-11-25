package executor_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
)

const testImage = "python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b"

func TestDockerExecutorRunsIsolatedExperimentAndCollectsArtifacts(t *testing.T) {
	requireDocker(t)
	root := filepath.Join(t.TempDir(), "workspaces")
	workspace := filepath.Join(root, "experiment-1")
	writeWorkspace(t, workspace, `import json, os, pathlib, socket
network_open = False
try:
    socket.create_connection(("1.1.1.1", 53), timeout=0.2).close()
    network_open = True
except OSError:
    pass
root_writable = True
try:
    pathlib.Path("/autoresearch-host-write").write_text("blocked")
except OSError:
    root_writable = False
pathlib.Path("output").mkdir(exist_ok=True)
metrics = {"answer": sum([1, 2, 3]), "network_open": network_open, "root_writable": root_writable, "host_secret": os.environ.get("AUTORESEARCH_HOST_SECRET", "")}
pathlib.Path("output/metrics.json").write_text(json.dumps(metrics))
print("experiment complete")
`)
	artifacts, err := artifact.New(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: root, AllowedImages: []string{testImage}, Artifacts: artifacts})
	if err != nil {
		t.Fatal(err)
	}
	job := executor.Job{ID: "job-1", RunID: "run", Workspace: workspace, Image: testImage, Command: []string{"python", "/work/experiment.py"}, Env: map[string]string{"SEED": "42"}, Timeout: 30 * time.Second, MemoryMB: 128, CPUs: 0.5, PIDs: 64}
	if err := runner.Submit(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Wait(context.Background(), job.ID)
	if err != nil || result.ExitCode != 0 || result.State != executor.StateCompleted {
		t.Fatalf("Wait() = %#v, %v", result, err)
	}
	content, err := os.ReadFile(filepath.Join(workspace, "output", "metrics.json"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(workspace, "output"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o002 == 0 {
		t.Fatalf("output directory is not host-cleanup-safe: %v", info.Mode())
	}
	var metrics map[string]any
	if err := json.Unmarshal(content, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics["answer"] != float64(6) || metrics["network_open"] != false || metrics["root_writable"] != false || metrics["host_secret"] != "" {
		t.Fatalf("metrics = %#v", metrics)
	}
	logs, err := runner.Logs(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var logText strings.Builder
	for entry := range logs {
		logText.WriteString(entry.Text)
	}
	if !strings.Contains(logText.String(), "experiment complete") {
		t.Fatalf("logs = %q", logText.String())
	}
	records, err := runner.Collect(context.Background(), job.ID)
	if err != nil || len(records) != 1 || records[0].SHA256 == "" {
		t.Fatalf("Collect() = %#v, %v", records, err)
	}
}

func TestDockerExecutorEnforcesTimeout(t *testing.T) {
	requireDocker(t)
	root := filepath.Join(t.TempDir(), "workspaces")
	workspace := filepath.Join(root, "timeout")
	writeWorkspace(t, workspace, "import time\ntime.sleep(60)\n")
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: root, AllowedImages: []string{testImage}})
	if err != nil {
		t.Fatal(err)
	}
	job := executor.Job{ID: "timeout", RunID: "run", Workspace: workspace, Image: testImage, Command: []string{"python", "/work/experiment.py"}, Timeout: 500 * time.Millisecond, MemoryMB: 128, CPUs: 0.5, PIDs: 64}
	if err := runner.Submit(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	result, err := runner.Wait(context.Background(), job.ID)
	if err != nil || result.State != executor.StateFailed || !strings.Contains(result.Error, "deadline") {
		t.Fatalf("Wait() = %#v, %v", result, err)
	}
}

func TestDockerExecutorRejectsWorkspaceEscapeAndUnknownImage(t *testing.T) {
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: t.TempDir(), AllowedImages: []string{testImage}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Submit(context.Background(), executor.Job{ID: "escape", Workspace: "/etc", Image: testImage, Command: []string{"true"}, Timeout: time.Second}); err == nil {
		t.Fatal("workspace escape was accepted")
	}
	if err := runner.Submit(context.Background(), executor.Job{ID: "image", Workspace: filepath.Join(t.TempDir(), "work"), Image: "alpine:latest", Command: []string{"true"}, Timeout: time.Second}); err == nil {
		t.Fatal("image outside allowlist was accepted")
	}
}

func writeWorkspace(t *testing.T, workspace, source string) {
	t.Helper()
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "experiment.py"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skipf("Docker daemon is unavailable: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "docker", "pull", testImage).CombinedOutput(); err != nil {
		t.Skipf("pinned test image is unavailable: %v: %s", err, output)
	}
}
