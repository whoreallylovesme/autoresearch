//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

type researchDaemonProcess struct {
	command  *exec.Cmd
	logs     *lockedBuffer
	baseURL  string
	waitOnce sync.Once
	waitErr  error
}

func startResearchDaemon(t *testing.T, root, binary, dataDir, address string, environment ...string) *researchDaemonProcess {
	t.Helper()
	command := exec.Command(binary, "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--data-dir", dataDir, "--listen", address)
	command.Dir = root
	command.Env = append(os.Environ(), environment...)
	logs := &lockedBuffer{}
	command.Stdout = logs
	command.Stderr = logs
	process := &researchDaemonProcess{
		command: command,
		logs:    logs,
		baseURL: "http://" + address,
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start researchd: %v", err)
	}
	t.Cleanup(process.close)
	return process
}

func (p *researchDaemonProcess) waitReady(t *testing.T) {
	t.Helper()
	waitForDaemon(t, p.baseURL)
}

func (p *researchDaemonProcess) wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.command.Wait()
	})
	return p.waitErr
}

func (p *researchDaemonProcess) close() {
	_ = p.command.Process.Kill()
	_ = p.wait()
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
