package executor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/artifact"
	"github.com/whoreallylovesme/autoresearch/internal/executor"
)

func TestJobIDValidationBeforeExecutorSideEffects(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{name: "one character", id: "a", valid: true},
		{name: "96 characters", id: strings.Repeat("a", 96), valid: true},
		{name: "empty", id: ""},
		{name: "current directory", id: "."},
		{name: "parent directory", id: ".."},
		{name: "slash", id: "job/part"},
		{name: "97 characters", id: strings.Repeat("a", 97)},
	}
	adapters := []struct {
		name     string
		exercise func(*testing.T, string, bool)
	}{
		{name: "Docker", exercise: exerciseDockerJobID},
		{name: "SSH", exercise: exerciseSSHJobID},
	}
	for _, adapter := range adapters {
		t.Run(adapter.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					adapter.exercise(t, test.id, test.valid)
				})
			}
		})
	}
}

func exerciseDockerJobID(t *testing.T, id string, valid bool) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "workspaces")
	workspace := filepath.Join(root, "workspace")
	binaryDirectory := t.TempDir()
	binary := filepath.Join(binaryDirectory, "docker")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n: > \"${0%/*}/called\"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binaryDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	runner, err := executor.NewDocker(executor.DockerOptions{WorkspaceRoot: root, AllowedImages: []string{testImage}})
	if err != nil {
		t.Fatal(err)
	}
	job := executor.Job{ID: id, RunID: "run", Workspace: workspace, Image: testImage, Command: []string{"true"}, Timeout: time.Second}
	err = runner.Submit(context.Background(), job)
	if valid {
		if err != nil {
			t.Fatalf("Submit() rejected valid job ID %q: %v", id, err)
		}
		waitForExecutorJob(t, runner, id)
		return
	}
	if err == nil {
		waitForExecutorJob(t, runner, id)
		t.Fatalf("Submit() accepted invalid job ID %q", id)
	}
	requireExecutorPathAbsent(t, workspace)
	requireExecutorPathAbsent(t, filepath.Join(binaryDirectory, "called"))
}

func exerciseSSHJobID(t *testing.T, id string, valid bool) {
	t.Helper()
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspaces")
	workspace := filepath.Join(workspaceRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	binaryDirectory := t.TempDir()
	binary := filepath.Join(binaryDirectory, "ssh")
	content := "#!/bin/bash\n: >> \"${0%/*}/called\"\nlast=\"${@: -1}\"\n/bin/bash -c \"$last\"\n"
	if err := os.WriteFile(binary, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := artifact.New(filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := filepath.Join(root, "remote")
	runner, err := executor.NewSSH(executor.SSHOptions{
		Binary: binary, WorkspaceRoot: workspaceRoot, RemoteRoot: remoteRoot,
		Destination: "research@example.invalid", AllowedDestinations: []string{"research@example.invalid"},
		IdentityFile: touchFile(t, "identity"), KnownHostsFile: touchFile(t, "known_hosts"), Artifacts: artifacts,
	})
	if err != nil {
		t.Fatal(err)
	}
	job := executor.Job{ID: id, RunID: "run", Workspace: workspace, Command: []string{"true"}, Timeout: time.Second}
	err = runner.Submit(context.Background(), job)
	if valid {
		if err != nil {
			t.Fatalf("Submit() rejected valid job ID %q: %v", id, err)
		}
		waitForExecutorJob(t, runner, id)
		return
	}
	if err == nil {
		waitForExecutorJob(t, runner, id)
		t.Fatalf("Submit() accepted invalid job ID %q", id)
	}
	requireExecutorPathAbsent(t, remoteRoot)
	requireExecutorPathAbsent(t, filepath.Join(binaryDirectory, "called"))
}

func waitForExecutorJob(t *testing.T, runner executor.Executor, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runner.Wait(ctx, id); err != nil {
		t.Fatalf("Wait() after Submit(): %v", err)
	}
}

func requireExecutorPathAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q was touched before job ID rejection", path)
	}
}
