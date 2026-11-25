package sandbox_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/sandbox"
)

func TestGitWorktreesIsolateConcurrentAgents(t *testing.T) {
	repository := t.TempDir()
	runGit(t, repository, "init")
	if err := os.WriteFile(filepath.Join(repository, "experiment.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repository, "add", "experiment.txt")
	runGit(t, repository, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	manager, err := sandbox.NewWorktrees(filepath.Join(t.TempDir(), "worktrees"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Create(context.Background(), "agent-1", repository, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Create(context.Background(), "agent-2", repository, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "experiment.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(second, "experiment.txt"))
	if err != nil || string(content) != "base\n" {
		t.Fatalf("second worktree = %q, %v", content, err)
	}
	runGit(t, first, "add", "experiment.txt")
	runGit(t, first, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "agent change")
	if err := manager.Remove(context.Background(), "agent-1"); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, repository string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repository}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
