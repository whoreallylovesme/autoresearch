//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestResearchDaemonRecoversAfterSIGKILL(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(t.TempDir(), "researchd")
	build(t, root, daemon, "./cmd/researchd")
	dataDir := t.TempDir()
	firstAddress := freeAddress(t)
	first := startResearchDaemon(t, root, daemon, dataDir, firstAddress, "AUTORESEARCH_DEMO_DELAY=2s")
	first.waitReady(t)
	response, err := http.Post(first.baseURL+"/api/v1/run/start", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	waitForCompletedTask(t, filepath.Join(dataDir, "autoresearch.db"), "research-a")
	if err := first.command.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = first.wait()
	if first.command.ProcessState.Success() {
		t.Fatal("first daemon exited before SIGKILL")
	}

	secondAddress := freeAddress(t)
	second := startResearchDaemon(t, root, daemon, dataDir, secondAddress)
	defer func() {
		t.Logf("first daemon logs: %s", first.logs.String())
		t.Logf("second daemon logs: %s", second.logs.String())
	}()
	second.waitReady(t)
	response, err = http.Post(second.baseURL+"/api/v1/run/start", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("restart status = %s; first logs = %s; second logs = %s", response.Status, first.logs.String(), second.logs.String())
	}
	response.Body.Close()
	waitForResearchState(t, "http://"+secondAddress, "completed")
	store, err := storage.Open(filepath.Join(dataDir, "autoresearch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Load(context.Background(), "deterministic-scheduling-demo")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run.State != "completed" || len(snapshot.Tasks) != 3 || len(snapshot.Messages) != 3 || len(snapshot.Artifacts) != 1 {
		t.Fatalf("daemon snapshot = %#v; first logs = %s; second logs = %s", snapshot, first.logs.String(), second.logs.String())
	}
	for _, task := range snapshot.Tasks {
		if task.State != scheduler.StateCompleted || countFinding(snapshot, task.ID) != 1 {
			t.Fatalf("task was lost or duplicated: %#v", task)
		}
	}
}

func waitForResearchState(t *testing.T, baseURL, expected string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/api/v1/run/status")
		if err == nil {
			var status map[string]any
			_ = json.NewDecoder(response.Body).Decode(&status)
			response.Body.Close()
			if status["state"] == expected {
				return
			}
			if status["state"] == "failed" || status["state"] == "cancelled" {
				t.Fatalf("research entered state %s", status["state"])
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("research did not reach %s", expected)
}
