//go:build integration

package integration_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResearchDaemonProcessReapsEarlyExitAndRetainsLogs(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(t.TempDir(), "researchd")
	build(t, root, daemon, "./cmd/researchd")

	process := startResearchDaemon(t, root, daemon, t.TempDir(), "invalid-address")
	waitErr := process.wait()
	if waitErr == nil {
		t.Fatal("researchd with an invalid listen address exited without an error")
	}
	if process.command.ProcessState == nil || !process.command.ProcessState.Exited() {
		t.Fatal("researchd process was not reaped")
	}
	if logs := process.logs.String(); !strings.Contains(logs, "researchd stopped") {
		t.Fatalf("researchd startup logs = %q", logs)
	}

	process.close()
	if cleanupErr := process.wait(); cleanupErr != waitErr {
		t.Fatalf("cleanup wait error = %v, original wait error = %v", cleanupErr, waitErr)
	}
	if logs := process.logs.String(); !strings.Contains(logs, "researchd stopped") {
		t.Fatalf("researchd logs after cleanup = %q", logs)
	}
}
