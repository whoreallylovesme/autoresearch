//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDaemonAndCLICompleteCredentialFreeResearch(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	daemon := filepath.Join(binDir, "researchd")
	control := filepath.Join(binDir, "researchctl")
	build(t, root, daemon, "./cmd/researchd")
	build(t, root, control, "./cmd/researchctl")
	address := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, daemon, "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--listen", address)
	command.Dir = root
	var daemonLogs strings.Builder
	command.Stdout = &daemonLogs
	command.Stderr = &daemonLogs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}()
	baseURL := "http://" + address
	waitForDaemon(t, baseURL)
	runCtx, runCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer runCancel()
	output, err := exec.CommandContext(runCtx, control, "run", "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--api", baseURL).CombinedOutput()
	if err != nil {
		t.Fatalf("researchctl run error = %v\noutput = %s\ndaemon = %s", err, output, daemonLogs.String())
	}
	if !strings.Contains(string(output), "Research Report") || !strings.Contains(string(output), "research-a") {
		t.Fatalf("run output = %s", output)
	}
	for _, args := range [][]string{{"status"}, {"agents"}, {"tasks"}, {"budget"}, {"report"}, {"logs", "--follow"}} {
		args = append(args, "--api", baseURL)
		output, err := exec.CommandContext(runCtx, control, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("researchctl %v error = %v\noutput = %s", args, err, output)
		}
		if len(output) == 0 {
			t.Fatalf("researchctl %v returned no output", args)
		}
	}
	output, err = exec.CommandContext(runCtx, control, "validate", "--config", filepath.Join(root, "configs", "autoresearch.yaml")).CombinedOutput()
	if err != nil || !strings.Contains(string(output), "configuration valid") {
		t.Fatalf("validate output = %s, error = %v", output, err)
	}
}

func build(t *testing.T, root, output, pkg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "build", "-o", output, pkg)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("go build %s error = %v\n%s", pkg, err, output)
	}
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForDaemon(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(baseURL + "/api/v1/run/status")
		if err == nil {
			var status map[string]any
			err = json.NewDecoder(response.Body).Decode(&status)
			response.Body.Close()
			if err == nil && status["state"] == "created" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("daemon at %s did not become ready", baseURL))
}
