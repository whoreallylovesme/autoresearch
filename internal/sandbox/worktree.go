package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
)

type Worktrees struct {
	root         string
	mu           sync.Mutex
	paths        map[string]string
	repositories map[string]string
}

func NewWorktrees(root string) (*Worktrees, error) {
	if root == "" {
		return nil, errors.New("worktree root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Worktrees{root: root, paths: map[string]string{}, repositories: map[string]string{}}, nil
}

func (w *Worktrees) Create(ctx context.Context, id, repository, revision string) (string, error) {
	if !regexp.MustCompile(`^[A-Za-z0-9._-]{1,96}$`).MatchString(id) || revision == "" {
		return "", errors.New("valid agent ID and revision are required")
	}
	if info, err := os.Stat(filepath.Join(repository, ".git")); err != nil || !info.IsDir() {
		return "", errors.New("repository has no Git metadata")
	}
	path := filepath.Join(w.root, id)
	w.mu.Lock()
	if _, exists := w.paths[id]; exists {
		w.mu.Unlock()
		return "", errors.New("worktree ID already exists")
	}
	if _, err := os.Stat(path); err == nil {
		w.mu.Unlock()
		return "", errors.New("worktree path already exists")
	}
	w.paths[id] = path
	w.repositories[id] = repository
	w.mu.Unlock()
	if output, err := exec.CommandContext(ctx, "git", "-C", repository, "worktree", "add", "--detach", path, revision).CombinedOutput(); err != nil {
		w.mu.Lock()
		delete(w.paths, id)
		delete(w.repositories, id)
		w.mu.Unlock()
		return "", errors.New("git worktree add: " + string(output))
	}
	return path, nil
}

func (w *Worktrees) Remove(ctx context.Context, id string) error {
	w.mu.Lock()
	path, ok := w.paths[id]
	w.mu.Unlock()
	if !ok {
		return errors.New("worktree not found")
	}
	repository := w.repositories[id]
	if output, err := exec.CommandContext(ctx, "git", "-C", repository, "worktree", "remove", path).CombinedOutput(); err != nil {
		return errors.New("git worktree remove: " + string(output))
	}
	w.mu.Lock()
	delete(w.paths, id)
	delete(w.repositories, id)
	w.mu.Unlock()
	return nil
}
