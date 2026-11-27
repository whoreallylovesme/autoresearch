package executortest

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

type daemonTest interface {
	Helper()
	Skipf(format string, args ...any)
}

func RequireDaemon(t testing.TB) {
	t.Helper()
	requireDaemon(t, exec.LookPath, func(ctx context.Context) error {
		return exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run()
	})
}

func requireDaemon(t daemonTest, lookPath func(string) (string, error), probe func(context.Context) error) {
	t.Helper()
	if _, err := lookPath("docker"); err != nil {
		t.Skipf("Docker unavailable: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := probe(ctx); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}
}
