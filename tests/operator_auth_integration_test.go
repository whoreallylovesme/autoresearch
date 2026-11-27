//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
)

const integrationTestToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func TestResearchDaemonStartupAuthenticationFailures(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(t.TempDir(), "researchd")
	build(t, root, daemon, "./cmd/researchd")
	for _, test := range []struct {
		name        string
		environment func(*testing.T) []string
		secrets     []string
	}{
		{name: "missing", environment: func(*testing.T) []string { return nil }},
		{name: "malformed", environment: func(*testing.T) []string {
			return []string{operatorauth.TokenEnvironment + "=not-a-valid-token"}
		}, secrets: []string{"not-a-valid-token"}},
		{name: "duplicate", environment: func(*testing.T) []string {
			return []string{
				operatorauth.TokenEnvironment + "=" + integrationTestToken,
				operatorauth.TokenFileEnvironment + "=unused",
			}
		}, secrets: []string{integrationTestToken}},
		{name: "unsafe token file", environment: func(t *testing.T) []string {
			credential, err := operatorauth.Generate()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "operator-token")
			if err := credential.WriteFile(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			return []string{operatorauth.TokenFileEnvironment + "=" + path}
		}},
		{name: "foreign-owned token file", environment: func(t *testing.T) []string {
			if os.Geteuid() != 0 {
				t.Skip("changing file ownership requires root")
			}
			path := filepath.Join(t.TempDir(), "operator-token")
			if err := os.WriteFile(path, []byte(integrationTestToken+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(path, 65532, -1); err != nil {
				t.Fatal(err)
			}
			return []string{operatorauth.TokenFileEnvironment + "=" + path}
		}, secrets: []string{integrationTestToken}},
	} {
		t.Run(test.name, func(t *testing.T) {
			address := freeAddress(t)
			dataDir := filepath.Join(t.TempDir(), "private-state")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, daemon, "--config", filepath.Join(root, "configs", "autoresearch.yaml"), "--data-dir", dataDir, "--listen", address)
			command.Dir = root
			command.Env = environmentWithoutOperatorCredentials(test.environment(t)...)
			var output bytes.Buffer
			command.Stdout = &output
			command.Stderr = &output
			runErr := command.Run()
			if runErr == nil || ctx.Err() != nil {
				t.Fatalf("researchd did not fail closed promptly: error = %v, context = %v", runErr, ctx.Err())
			}
			for _, secret := range test.secrets {
				if strings.Contains(output.String(), secret) {
					t.Fatal("researchd startup output leaked the operator token")
				}
			}
			if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
				t.Fatal("researchd initialized state before rejecting its operator credential")
			}
			listener, err := net.Listen("tcp", address)
			if err != nil {
				t.Fatalf("researchd retained or opened the listener: %v", err)
			}
			listener.Close()
		})
	}
}

func TestResearchDaemonAuthenticationBoundaries(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	daemon := filepath.Join(t.TempDir(), "researchd")
	build(t, root, daemon, "./cmd/researchd")
	process := startResearchDaemon(t, root, daemon, t.TempDir(), freeAddress(t))
	process.waitReady(t)

	response, err := http.Get(process.baseURL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("public health status = %d", response.StatusCode)
	}

	for _, route := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/v1/run/start"},
		{method: http.MethodPost, path: "/api/v1/run/stop"},
		{method: http.MethodPost, path: "/api/v1/run/pause"},
		{method: http.MethodPost, path: "/api/v1/run/resume"},
		{method: http.MethodPost, path: "/api/v1/run/finalize"},
		{method: http.MethodPost, path: "/api/v1/chat"},
		{method: http.MethodGet, path: "/api/v1/run/status"},
		{method: http.MethodGet, path: "/api/v1/agents"},
		{method: http.MethodGet, path: "/api/v1/messages"},
		{method: http.MethodGet, path: "/api/v1/tasks"},
		{method: http.MethodGet, path: "/api/v1/budget"},
		{method: http.MethodGet, path: "/api/v1/events"},
		{method: http.MethodGet, path: "/api/v1/events/stream"},
		{method: http.MethodGet, path: "/api/v1/report"},
		{method: http.MethodGet, path: "/api/v1/graph"},
		{method: http.MethodGet, path: "/api/v1/papers"},
		{method: http.MethodGet, path: "/api/v1/experiments"},
		{method: http.MethodGet, path: "/api/v1/playback"},
		{method: http.MethodGet, path: "/metrics"},
		{method: http.MethodGet, path: "/future-route"},
		{method: http.MethodPost, path: "/healthz"},
		{method: http.MethodGet, path: "/healthz/"},
		{method: http.MethodGet, path: "/healthz?verbose=true"},
	} {
		request, err := http.NewRequest(route.method, process.baseURL+route.path, strings.NewReader("not read"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		assertIntegrationUnauthorized(t, response)
	}

	validToken, err := os.ReadFile(process.tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	wrongCredential, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		header func(*http.Request)
	}{
		{name: "malformed", header: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer malformed")
		}},
		{name: "wrong", header: func(request *http.Request) {
			if err := wrongCredential.SetBearer(request); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "duplicate", header: func(request *http.Request) {
			request.Header["Authorization"] = []string{"Bearer " + strings.TrimSpace(string(validToken)), "Bearer malformed"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet, process.baseURL+"/api/v1/run/status", nil)
			if err != nil {
				t.Fatal(err)
			}
			test.header(request)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			assertIntegrationUnauthorized(t, response)
		})
	}

	response, err = process.request(http.MethodGet, "/future-route", nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("authenticated future route status = %d", response.StatusCode)
	}
}

func TestCLIRejectsCredentialForwardingRedirects(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(t.TempDir(), "researchctl")
	build(t, root, control, "./cmd/researchctl")
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		forwarded.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+integrationTestToken {
			t.Error("CLI source request is missing its bearer")
		}
		http.Redirect(writer, request, target.URL+"/forwarded", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	for _, args := range [][]string{{"status"}, {"logs", "--follow"}} {
		args = append(args, "--api", source.URL)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, control, args...)
		command.Env = environmentWithoutOperatorCredentials(operatorauth.TokenEnvironment + "=" + integrationTestToken)
		output, err := command.CombinedOutput()
		contextErr := ctx.Err()
		cancel()
		if err == nil || contextErr != nil {
			t.Fatalf("researchctl %v redirect result: error = %v, context = %v", args, err, contextErr)
		}
		if strings.Contains(string(output), integrationTestToken) {
			t.Fatal("CLI redirect failure printed the operator token")
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("CLI forwarded credentials to a redirect destination")
	}
}

func assertIntegrationUnauthorized(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if response.Header.Get("WWW-Authenticate") != "Bearer" || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("unauthorized response is missing its challenge or no-store policy")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != "unauthorized" {
		t.Fatal("unauthorized response is not generic")
	}
}
