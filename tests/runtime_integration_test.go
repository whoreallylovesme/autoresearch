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

func TestDaemonAndCLICompleteAuthenticatedResearch(t *testing.T) {
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
	daemonProcess := startResearchDaemon(t, root, daemon, filepath.Join(binDir, "private-data"), address)
	daemonProcess.waitReady(t)
	baseURL := daemonProcess.baseURL
	runCtx, runCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer runCancel()
	command := exec.CommandContext(runCtx, control, "run", "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--api", baseURL)
	command.Env = operatorEnvironmentWithFile(daemonProcess.tokenPath)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("researchctl run error = %v\noutput = %s\ndaemon = %s", err, output, daemonProcess.logs.String())
	}
	if !strings.Contains(string(output), "Research Report") || !strings.Contains(string(output), "research-a") {
		t.Fatalf("run output = %s", output)
	}
	for _, args := range [][]string{{"status"}, {"agents"}, {"tasks"}, {"budget"}, {"report"}, {"logs", "--follow"}} {
		args = append(args, "--api", baseURL)
		command := exec.CommandContext(runCtx, control, args...)
		command.Env = operatorEnvironmentWithFile(daemonProcess.tokenPath)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("researchctl %v error = %v\noutput = %s", args, err, output)
		}
		if len(output) == 0 {
			t.Fatalf("researchctl %v returned no output", args)
		}
	}
	command = exec.CommandContext(runCtx, control, "validate", "--config", filepath.Join(root, "configs", "autoresearch.yaml"))
	command.Env = environmentWithoutOperatorCredentials()
	output, err = command.CombinedOutput()
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

func waitForDaemon(t *testing.T, process *researchDaemonProcess) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastStatus map[string]any
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := process.request(http.MethodGet, "/api/v1/run/status", nil)
		lastErr = err
		if err == nil {
			err = json.NewDecoder(response.Body).Decode(&lastStatus)
			response.Body.Close()
			lastErr = err
			if err == nil && lastStatus["state"] != "" {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("daemon at %s did not become ready: last status = %#v, error = %v", process.baseURL, lastStatus, lastErr))
}
