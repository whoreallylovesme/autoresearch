package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/operatorauth"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

const cliTestToken = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

func TestAPIClientAttachesBearerToREST(t *testing.T) {
	useCLIEnvironmentToken(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+cliTestToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()
	client, err := newAPIClient()
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]bool
	if err := client.call(context.Background(), http.MethodGet, server.URL+"/api/v1/run/status", nil, &result); err != nil {
		t.Fatal(err)
	}
	if !result["ok"] {
		t.Fatal("authenticated REST response was not decoded")
	}
}

func TestAPIClientLoadsBearerFromFile(t *testing.T) {
	clearCLIOperatorEnvironment(t)
	credential, err := operatorauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operator-token")
	if err := credential.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	t.Setenv(operatorauth.TokenFileEnvironment, path)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !credential.Authorize(request) {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := newAPIClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.call(context.Background(), http.MethodPost, server.URL+"/api/v1/run/stop", nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAPIClientAttachesBearerToPolling(t *testing.T) {
	useCLIEnvironmentToken(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+cliTestToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		requests.Add(1)
		switch request.URL.Path {
		case "/api/v1/run/status":
			_ = json.NewEncoder(writer).Encode(research.Status{State: research.StateCompleted})
		case "/api/v1/report":
			_ = json.NewEncoder(writer).Encode(map[string]string{"report": "verified report"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := newAPIClient()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := client.waitForReport(context.Background(), server.URL, &output); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || !strings.Contains(output.String(), "verified report") {
		t.Fatalf("authenticated polling made %d requests and produced %q", requests.Load(), output.String())
	}
}

func TestAPIClientAttachesBearerToSSE(t *testing.T) {
	useCLIEnvironmentToken(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+cliTestToken {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintln(writer, "data: {\"sequence\":1}")
	}))
	defer server.Close()
	client, err := newAPIClient()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := client.streamEvents(context.Background(), server.URL, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\"sequence\":1") {
		t.Fatalf("SSE output = %q", output.String())
	}
}

func TestAPIClientRejectsRESTAndSSERedirects(t *testing.T) {
	useCLIEnvironmentToken(t)
	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+cliTestToken {
			t.Error("source request is missing operator bearer")
		}
		http.Redirect(writer, request, target.URL+"/forwarded", http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client, err := newAPIClient()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.call(context.Background(), http.MethodGet, source.URL+"/api/v1/events", nil, nil); err == nil {
		t.Fatal("REST client followed an authenticated redirect")
	} else if strings.Contains(err.Error(), cliTestToken) {
		t.Fatal("REST redirect error leaked the operator token")
	}
	if err := client.streamEvents(context.Background(), source.URL, &bytes.Buffer{}); err == nil {
		t.Fatal("SSE client followed an authenticated redirect")
	} else if strings.Contains(err.Error(), cliTestToken) {
		t.Fatal("SSE redirect error leaked the operator token")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatal("redirect destination received an authenticated request")
	}
}

func TestAPIClientRequiresExactlyOneCredentialSource(t *testing.T) {
	clearCLIOperatorEnvironment(t)
	if _, err := newAPIClient(); err == nil {
		t.Fatal("API client initialized without credentials")
	}
	t.Setenv(operatorauth.TokenEnvironment, cliTestToken)
	t.Setenv(operatorauth.TokenFileEnvironment, "duplicate")
	if _, err := newAPIClient(); err == nil {
		t.Fatal("API client initialized with duplicate credential sources")
	}
}

func TestOfflineCommandsRemainCredentialFree(t *testing.T) {
	clearCLIOperatorEnvironment(t)
	project := filepath.Join(t.TempDir(), "project")
	root := newRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"init", "--directory", project})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root = newRootCommand()
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"validate", "--config", filepath.Join(project, "configs", "autoresearch.yaml")})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func useCLIEnvironmentToken(t *testing.T) {
	t.Helper()
	clearCLIOperatorEnvironment(t)
	t.Setenv(operatorauth.TokenEnvironment, cliTestToken)
}

func clearCLIOperatorEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{operatorauth.TokenEnvironment, operatorauth.TokenFileEnvironment} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}
