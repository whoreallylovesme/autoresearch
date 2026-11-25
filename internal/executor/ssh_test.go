package executor_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
)

func TestSSHExecutorRunsRemoteJobAndCollectsArtifacts(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspaces", "remote-job")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "experiment.py"), []byte("import pathlib\npathlib.Path('output').mkdir(exist_ok=True)\npathlib.Path('output/result.txt').write_text('remote result')\nprint('remote complete')\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.New(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	runner, err := executor.NewSSH(executor.SSHOptions{
		Binary: fakeSSH(t), WorkspaceRoot: filepath.Join(root, "workspaces"), RemoteRoot: filepath.Join(root, "remote"),
		Destination: "research@example.invalid", AllowedDestinations: []string{"research@example.invalid"},
		IdentityFile: touchFile(t, "identity"), KnownHostsFile: touchFile(t, "known_hosts"), Artifacts: artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	job := executor.Job{ID: "remote-job", RunID: "run", Workspace: workspace, Command: []string{"python3", "/work/experiment.py"}, Timeout: 10 * time.Second}
	if err := runner.Submit(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	waitDone := make(chan executor.Result, 1)
	go func() {
		result, _ := runner.Wait(context.Background(), job.ID)
		waitDone <- result
	}()
	var result executor.Result
	for {
		status, err := runner.Status(context.Background(), job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State != executor.StateRunning {
			result = <-waitDone
			break
		}
		time.Sleep(time.Millisecond)
	}
	if result.State != executor.StateCompleted || result.ExitCode != 0 {
		t.Fatalf("Wait() = %#v", result)
	}
	logs, err := runner.Logs(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for entry := range logs {
		text.WriteString(entry.Text)
	}
	if !strings.Contains(text.String(), "remote complete") {
		t.Fatalf("logs = %q", text.String())
	}
	records, err := runner.Collect(context.Background(), job.ID)
	if err != nil || len(records) != 1 {
		t.Fatalf("Collect() = %#v, %v", records, err)
	}
}

func TestSSHRejectsUnapprovedDestination(t *testing.T) {
	_, err := executor.NewSSH(executor.SSHOptions{WorkspaceRoot: t.TempDir(), RemoteRoot: "/tmp/research", Destination: "root@unapproved", AllowedDestinations: []string{"research@approved"}, IdentityFile: touchFile(t, "identity"), KnownHostsFile: touchFile(t, "known_hosts")})
	if err == nil {
		t.Fatal("unapproved destination was accepted")
	}
}

func fakeSSH(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh")
	content := "#!/bin/bash\nlast=\"${@: -1}\"\n/bin/bash -c \"$last\"\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func touchFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("test-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
