package executortest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type recordingT struct {
	skipMessage string
}

func (t *recordingT) Helper() {}

func (t *recordingT) Skipf(format string, args ...any) {
	t.skipMessage = fmt.Sprintf(format, args...)
}

func TestRequireDaemonRunsOneBoundedProbe(t *testing.T) {
	recorder := &recordingT{}
	lookPathCalls := 0
	probeCalls := 0

	requireDaemon(recorder, func(file string) (string, error) {
		lookPathCalls++
		if file != "docker" {
			t.Fatalf("LookPath file = %q", file)
		}
		return "/usr/bin/docker", nil
	}, func(ctx context.Context) error {
		probeCalls++
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("probe context has no deadline")
		} else if remaining := time.Until(deadline); remaining < 4*time.Second || remaining > 5*time.Second {
			t.Errorf("probe timeout = %v, want 5s", remaining)
		}
		return nil
	})

	if lookPathCalls != 1 {
		t.Fatalf("LookPath calls = %d, want 1", lookPathCalls)
	}
	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", probeCalls)
	}
	if recorder.skipMessage != "" {
		t.Fatalf("skip message = %q, want no skip", recorder.skipMessage)
	}
}

func TestRequireDaemonSkipsWithoutProbeWhenCLIIsMissing(t *testing.T) {
	recorder := &recordingT{}
	probeCalls := 0

	requireDaemon(recorder, func(string) (string, error) {
		return "", errors.New("no docker")
	}, func(context.Context) error {
		probeCalls++
		return nil
	})

	if probeCalls != 0 {
		t.Fatalf("probe calls = %d, want 0", probeCalls)
	}
	if recorder.skipMessage != "Docker unavailable: no docker" {
		t.Fatalf("skip message = %q", recorder.skipMessage)
	}
}

func TestRequireDaemonSkipsWhenDaemonProbeFails(t *testing.T) {
	recorder := &recordingT{}
	probeCalls := 0

	requireDaemon(recorder, func(string) (string, error) {
		return "/usr/bin/docker", nil
	}, func(context.Context) error {
		probeCalls++
		return errors.New("daemon stopped")
	})

	if probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", probeCalls)
	}
	if recorder.skipMessage != "Docker daemon unavailable: daemon stopped" {
		t.Fatalf("skip message = %q", recorder.skipMessage)
	}
}
