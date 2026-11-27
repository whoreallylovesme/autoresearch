//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
)

type researchDaemonProcess struct {
	command    *exec.Cmd
	logs       *lockedBuffer
	baseURL    string
	credential operatorauth.Token
	tokenPath  string
	waitOnce   sync.Once
	waitErr    error
}

func startResearchDaemon(t *testing.T, root, binary, dataDir, address string, environment ...string) *researchDaemonProcess {
	t.Helper()
	credential, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(t.TempDir(), "operator-token")
	if err := credential.WriteFile(tokenPath); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--data-dir", dataDir, "--listen", address)
	command.Dir = root
	command.Env = operatorEnvironmentWithFile(tokenPath, environment...)
	logs := &lockedBuffer{}
	command.Stdout = logs
	command.Stderr = logs
	process := &researchDaemonProcess{
		command:    command,
		logs:       logs,
		baseURL:    "http://" + address,
		credential: credential,
		tokenPath:  tokenPath,
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start researchd: %v", err)
	}
	t.Cleanup(process.close)
	return process
}

func (p *researchDaemonProcess) waitReady(t *testing.T) {
	t.Helper()
	waitForDaemon(t, p)
}

func (p *researchDaemonProcess) request(method, path string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest(method, p.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if err := p.credential.SetBearer(request); err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(request)
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

func operatorEnvironmentWithFile(path string, extra ...string) []string {
	return append(environmentWithoutOperatorCredentials(), append([]string{operatorauth.TokenFileEnvironment + "=" + path}, extra...)...)
}

func environmentWithoutOperatorCredentials(extra ...string) []string {
	environment := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == operatorauth.TokenEnvironment || name == operatorauth.TokenFileEnvironment {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, extra...)
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
