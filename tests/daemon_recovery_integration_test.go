//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	configPath := filepath.Join(root, "configs", "autoresearch.yaml")
	firstAddress := freeAddress(t)
	firstLogs := &lockedBuffer{}
	first := exec.Command(daemon, "--config", configPath, "--data-dir", dataDir, "--listen", firstAddress)
	first.Env = append(os.Environ(), "AUTORESEARCH_DEMO_DELAY=2s")
	first.Stdout = firstLogs
	first.Stderr = firstLogs
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	waitForDaemon(t, "http://"+firstAddress)
	response, err := http.Post("http://"+firstAddress+"/api/v1/run/start", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	waitForCompletedTask(t, filepath.Join(dataDir, "autoresearch.db"), "research-a")
	if err := first.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	if first.ProcessState.Success() {
		t.Fatal("first daemon exited before SIGKILL")
	}

	secondAddress := freeAddress(t)
	secondLogs := &lockedBuffer{}
	second := exec.Command(daemon, "--config", configPath, "--data-dir", dataDir, "--listen", secondAddress)
	second.Stdout = secondLogs
	second.Stderr = secondLogs
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		t.Logf("first daemon logs: %s", firstLogs.String())
		t.Logf("second daemon logs: %s", secondLogs.String())
		_ = second.Process.Kill()
		_ = second.Wait()
	}()
	waitForDaemon(t, "http://"+secondAddress)
	response, err = http.Post("http://"+secondAddress+"/api/v1/run/start", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("restart status = %s; first logs = %s; second logs = %s", response.Status, firstLogs.String(), secondLogs.String())
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
		t.Fatalf("daemon snapshot = %#v; first logs = %s; second logs = %s", snapshot, firstLogs.String(), secondLogs.String())
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

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(content)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *lockedBuffer) GoString() string { return fmt.Sprintf("%q", b.String()) }
