//go:build nativekimi

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func TestNativeKimiWebCreatesDistinctSessions(t *testing.T) {
	tokenPath := os.Getenv("AUTORESEARCH_KIMI_WEB_TOKEN_FILE")
	if tokenPath == "" {
		tokenPath = "/tmp/kimi-code/server.token"
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("native Kimi token is unavailable: %v", err)
	}
	workspace := os.Getenv("AUTORESEARCH_KIMI_WEB_WORKSPACE")
	if workspace == "" {
		workspace = t.TempDir()
	}
	newProvider := func(agentID string) *provider.KimiWebProvider {
		client, err := provider.NewKimiWeb(provider.KimiWebOptions{Token: strings.TrimSpace(string(token)), BaseURL: nativeKimiURL(), AgentID: agentID, Workspace: workspace})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first := newProvider("smoke-worker-1")
	second := newProvider("smoke-worker-2")
	if err := first.Start(ctx); err != nil {
		t.Fatalf("first native session failed: %v", err)
	}
	if err := second.Start(ctx); err != nil {
		t.Fatalf("second native session failed: %v", err)
	}
	if first.SessionID() == "" || second.SessionID() == "" || first.SessionID() == second.SessionID() {
		t.Fatalf("sessions were not isolated: %q vs %q", first.SessionID(), second.SessionID())
	}
	tokenValue := strings.TrimSpace(string(token))
	nativeWorkspace(t, ctx, tokenValue, first.SessionID(), workspace, "smoke-worker-1")
	nativeWorkspace(t, ctx, tokenValue, second.SessionID(), workspace, "smoke-worker-2")
}

func nativeWorkspace(t *testing.T, ctx context.Context, token, sessionID, workspace, agentID string) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, nativeKimiURL()+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("read native session: HTTP %d", response.StatusCode)
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			WorkspaceRoot string `json:"workspace_root"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(workspace, agentID)
	if payload.Code != 0 || payload.Data.WorkspaceRoot != expected {
		t.Fatalf("native session workspace = %q, want %q (code %d)", payload.Data.WorkspaceRoot, expected, payload.Code)
	}
}

func nativeKimiURL() string {
	if value := os.Getenv("AUTORESEARCH_KIMI_WEB_URL"); value != "" {
		return value
	}
	return "http://127.0.0.1:58627"
}
