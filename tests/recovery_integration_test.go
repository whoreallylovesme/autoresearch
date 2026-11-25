//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
	"github.com/whoreallylovesme/autoresearch/internal/storage"
)

func TestForcedTerminationRecovery(t *testing.T) {
	if mode := os.Getenv("AUTORESEARCH_RECOVERY_HELPER"); mode != "" {
		runRecoveryHelper(mode)
		return
	}
	dataDir := t.TempDir()
	ready := filepath.Join(dataDir, "second-task-started")
	first := helperCommand(t, "block", dataDir, ready)
	var firstOutput bytes.Buffer
	first.Stdout = &firstOutput
	first.Stderr = &firstOutput
	defer func() { t.Logf("first helper logs: %s", firstOutput.String()) }()
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	waitForCompletedTask(t, filepath.Join(dataDir, "autoresearch.db"), "research-a")
	if err := first.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	state := first.ProcessState
	firstLogs := firstOutput.String()
	if state.Success() {
		t.Fatalf("blocked helper exited before SIGKILL: %s", firstLogs)
	}
	second := helperCommand(t, "recover", dataDir, ready)
	output, err := second.CombinedOutput()
	if err != nil {
		t.Fatalf("recovery helper error = %v\noutput = %s\nfirst = %s", err, output, firstLogs)
	}
	store, err := storage.Open(filepath.Join(dataDir, "autoresearch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Load(context.Background(), "deterministic-scheduling-demo")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Run == nil || snapshot.Run.State != "completed" || len(snapshot.Tasks) != 3 {
		t.Fatalf("recovered snapshot = %#v", snapshot)
	}
	for _, task := range snapshot.Tasks {
		if task.State != scheduler.StateCompleted {
			t.Fatalf("recovered task = %#v", task)
		}
	}
	for _, taskID := range []string{"research-a", "research-b", "critic-review"} {
		if countFinding(snapshot, taskID) != 1 {
			t.Fatalf("finding count for %s is not one: %#v", taskID, snapshot.Findings)
		}
	}
	sequences := map[uint64]bool{}
	for _, event := range snapshot.Events {
		if event.ID == "" || event.SchemaVersion != 1 || sequences[event.Sequence] {
			t.Fatalf("invalid event history: %#v", event)
		}
		sequences[event.Sequence] = true
	}
	if snapshot.Budget == nil || len(snapshot.Budget.OpenReservations) != 0 {
		t.Fatalf("recovered budget = %#v", snapshot.Budget)
	}
}

func runRecoveryHelper(mode string) {
	configuration, err := config.Load("../configs/autoresearch.yaml")
	if err != nil {
		fatalHelper(err)
	}
	configuration.Runtime.DataDir = os.Getenv("AUTORESEARCH_RECOVERY_DATA")
	factory := research.DemoFactory()
	if mode == "block" {
		base := factory
		var calls atomic.Int32
		factory = func(agentID string, agentConfig config.AgentConfig) (provider.Provider, error) {
			if agentConfig.Role != "baby-scientist" {
				return base(agentID, agentConfig)
			}
			return provider.NewMockHandler(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
				if calls.Add(1) == 2 {
					if err := os.WriteFile(filepath.Join(configuration.Runtime.DataDir, "second-task-started"), []byte("ready"), 0o600); err != nil {
						return provider.Response{}, err
					}
					<-ctx.Done()
					return provider.Response{}, ctx.Err()
				}
				return provider.Response{Content: "durable finding from " + agentID, Usage: provider.Usage{InputTokens: 1, OutputTokens: 1}}, nil
			}), nil
		}
	}
	runtime, err := research.NewPersistent(configuration, factory)
	if err != nil {
		fatalHelper(err)
	}
	defer runtime.Close()
	if err := runtime.Run(context.Background()); err != nil {
		fatalHelper(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(runtime.Status()); err != nil {
		fatalHelper(err)
	}
}

func helperCommand(t *testing.T, mode, dataDir, _ string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=TestForcedTerminationRecovery")
	command.Env = append(os.Environ(), "AUTORESEARCH_RECOVERY_HELPER="+mode, "AUTORESEARCH_RECOVERY_DATA="+dataDir)
	return command
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear", path)
}

func waitForCompletedTask(t *testing.T, database, taskID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		store, err := storage.Open(database)
		if err == nil {
			snapshot, loadErr := store.Load(context.Background(), "deterministic-scheduling-demo")
			store.Close()
			for _, task := range snapshot.Tasks {
				if loadErr == nil && task.ID == taskID && task.State == scheduler.StateCompleted {
					return
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s was not durably completed", taskID)
}

func countFinding(snapshot storage.Snapshot, taskID string) int {
	count := 0
	for _, finding := range snapshot.Findings {
		if finding.TaskID == taskID {
			count++
		}
	}
	return count
}

func fatalHelper(err error) {
	if err == nil {
		err = errors.New("helper failed")
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
