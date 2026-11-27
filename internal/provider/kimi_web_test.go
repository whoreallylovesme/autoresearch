package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

type nativeSession struct {
	workspace string
	prompts   []string
	responses map[string]string
	wait      bool
}

func TestKimiWebCreatesIndependentSessionPerAgent(t *testing.T) {
	var mu sync.Mutex
	nextSession := 0
	sessions := map[string]*nativeSession{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing Kimi Web bearer token")
		}
		mu.Lock()
		defer mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writeNative := func(value any) { _ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": value}) }
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/sessions":
			nextSession++
			var body struct {
				WorkspaceRoot string         `json:"workspace_root"`
				AgentConfig   map[string]any `json:"agent_config"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.WorkspaceRoot == "" || body.AgentConfig["permission_mode"] != "manual" {
				t.Errorf("unexpected session creation: %#v", body)
			}
			sessionID := fmt.Sprintf("session-%d", nextSession)
			sessions[sessionID] = &nativeSession{workspace: body.WorkspaceRoot, responses: map[string]string{}}
			writeNative(map[string]any{"id": sessionID, "workspace_root": body.WorkspaceRoot})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/prompts"):
			sessionID := path.Base(strings.TrimSuffix(request.URL.Path, "/prompts"))
			session := sessions[sessionID]
			var body struct {
				PromptID string           `json:"prompt_id"`
				Content  []map[string]any `json:"content"`
				Metadata json.RawMessage  `json:"metadata"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if len(body.Metadata) > 0 {
				t.Errorf("empty metadata must be omitted, got %s", body.Metadata)
			}
			session.prompts = append(session.prompts, body.PromptID)
			text, _ := body.Content[0]["text"].(string)
			if sessionID == "session-1" {
				switch body.PromptID {
				case "task-1":
					if !strings.Contains(text, "Research objective") || !strings.Contains(text, "query_knowledge") {
						t.Errorf("first prompt lacks objective or runtime tools: %q", text)
					}
					session.responses[body.PromptID] = `{"tool_calls":[{"id":"call-1","name":"query_knowledge","arguments":{"query":"evidence"}}]}`
				case "task-2":
					if !strings.Contains(text, "bounded evidence") || strings.Contains(text, "Research objective") {
						t.Errorf("follow-up repeated native history or omitted tool result: %q", text)
					}
					session.responses[body.PromptID] = `{"final_response":"verified worker finding"}`
				default:
					t.Errorf("unexpected first worker prompt %q", body.PromptID)
				}
			} else {
				session.responses[body.PromptID] = `{"final_response":"second worker independent finding"}`
			}
			writeNative(map[string]any{"prompt_id": body.PromptID, "status": "running"})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":wait"):
			sessionID := path.Base(strings.TrimSuffix(request.URL.Path, ":wait"))
			writeNative(map[string]any{"timed_out": false, "pending_interactions": []any{}, "session": map[string]any{"id": sessionID, "activity": "idle", "last_turn_outcome": "completed"}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/agents/main/turns"):
			sessionID := strings.Split(strings.TrimSuffix(request.URL.Path, "/agents/main/turns"), "/api/v1/sessions/")[1]
			session := sessions[sessionID]
			promptID := session.prompts[len(session.prompts)-1]
			writeNative(map[string]any{"turns": []any{
				map[string]any{"ordinal": 0, "status": "ended", "items": []any{map[string]any{"type": "assistant_message", "text": "stale message from another turn"}}},
				map[string]any{"ordinal": 1, "status": "ended", "outcome": "completed", "items": []any{map[string]any{"type": "user_message", "prompt_ids": []string{promptID}}, map[string]any{"type": "assistant_message", "text": session.responses[promptID]}}},
			}})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":cancel"):
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["include_background"] != true {
				t.Errorf("cancel did not include background work: %#v", body)
			}
			writeNative(map[string]any{"cancelled": true})
		default:
			t.Errorf("unexpected request %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	newProvider := func(agentID string) *provider.KimiWebProvider {
		client, err := provider.NewKimiWeb(provider.KimiWebOptions{Model: "kimi-code/kimi-for-coding", BaseURL: server.URL, Token: "test-token", AgentID: agentID, Workspace: t.TempDir(), HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	first := newProvider("worker-1")
	second := newProvider("worker-2")
	messages := []provider.Message{{Role: "system", Content: "Scientific worker"}, {Role: "user", Content: "Research objective: test native isolation"}}
	tools := []provider.ToolDefinition{{Name: "query_knowledge", Description: "Read evidence", Parameters: json.RawMessage(`{"type":"object"}`)}}
	response, err := first.Generate(context.Background(), provider.Request{IdempotencyKey: "task-1", Messages: messages, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "query_knowledge" || string(response.ToolCalls[0].Arguments) != `{"query":"evidence"}` {
		t.Fatalf("runtime tool calls = %#v", response.ToolCalls)
	}
	messages = append(messages, provider.Message{Role: "assistant", ToolCalls: response.ToolCalls}, provider.Message{Role: "tool", ToolCallID: "call-1", Content: `{"evidence":"bounded evidence"}`})
	response, err = first.Generate(context.Background(), provider.Request{IdempotencyKey: "task-2", Messages: messages, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if response.Content != "verified worker finding" || len(response.ToolCalls) != 0 {
		t.Fatalf("first final response = %#v", response)
	}
	secondResponse, err := second.Generate(context.Background(), provider.Request{IdempotencyKey: "task-3", Messages: messages[:2], Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if secondResponse.Content != "second worker independent finding" {
		t.Fatalf("second response = %q", secondResponse.Content)
	}
	if first.SessionID() == second.SessionID() || first.SessionID() == "" {
		t.Fatalf("session IDs were shared: %q vs %q", first.SessionID(), second.SessionID())
	}
	if sessions["session-1"].workspace == sessions["session-2"].workspace {
		t.Fatalf("session workspaces were shared: %#v", sessions)
	}
	if !first.Capabilities().Tools || first.Capabilities().Streaming || first.Capabilities().StructuredOutputs {
		t.Fatal("Kimi Web capabilities do not describe the runtime tool bridge")
	}
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestKimiWebRejectsUnverifiedToolResponse(t *testing.T) {
	server := newNativeServer(t, `not a runtime response`, false)
	defer server.Close()
	client := newNativeClient(t, server)
	_, err := client.Generate(context.Background(), provider.Request{IdempotencyKey: "task-1", Tools: []provider.ToolDefinition{{Name: "query_knowledge"}}})
	if err == nil || !strings.Contains(err.Error(), "runtime response") {
		t.Fatalf("unstructured tool response error = %v", err)
	}
}

func TestKimiWebDoesNotApproveNativeInteractions(t *testing.T) {
	server := newNativeServer(t, `{"final_response":"must not be accepted"}`, true)
	defer server.Close()
	client := newNativeClient(t, server)
	_, err := client.Generate(context.Background(), provider.Request{IdempotencyKey: "task-1"})
	if err == nil || !strings.Contains(err.Error(), "approval") || strings.Contains(err.Error(), "test-token") {
		t.Fatalf("pending interaction error = %v", err)
	}
}

func TestKimiWebCancellationCancelsNativeTurn(t *testing.T) {
	var cancelled chan struct{}
	var workspace string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writeNative := func(value any) { _ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": value}) }
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/sessions":
			var body struct {
				WorkspaceRoot string `json:"workspace_root"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			workspace = body.WorkspaceRoot
			writeNative(map[string]any{"id": "session-1", "workspace_root": workspace})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/prompts"):
			writeNative(map[string]any{"prompt_id": "task-1", "status": "running"})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":wait"):
			writeNative(map[string]any{"timed_out": true, "pending_interactions": []any{}, "session": map[string]any{"activity": "turn"}})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":cancel"):
			close(cancelled)
			writeNative(map[string]any{"cancelled": true})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	cancelled = make(chan struct{})
	defer server.Close()
	client := newNativeClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := client.Generate(ctx, provider.Request{IdempotencyKey: "task-1"})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("cancellation error = %v, context = %v", err, ctx.Err())
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("native turn was not cancelled")
	}
}

func TestKimiWebRejectsTrailingRuntimeData(t *testing.T) {
	server := newNativeServer(t, `{"final_response":"unverified"} trailing-data`, false)
	defer server.Close()
	client := newNativeClient(t, server)
	_, err := client.Generate(context.Background(), provider.Request{IdempotencyKey: "task-1", Tools: []provider.ToolDefinition{{Name: "query_knowledge"}}})
	if err == nil || !strings.Contains(err.Error(), "runtime response") {
		t.Fatalf("trailing response data error = %v", err)
	}
}

func TestKimiWebRetriesTransientServerFailure(t *testing.T) {
	server := newNativeServer(t, `{"final_response":"recovered"}`, false, 1)
	defer server.Close()
	client := newNativeClient(t, server)
	response, err := client.Generate(context.Background(), provider.Request{IdempotencyKey: "task-1", Tools: []provider.ToolDefinition{{Name: "query_knowledge"}}})
	if err != nil || response.Content != "recovered" {
		t.Fatalf("retry response = %#v, %v", response, err)
	}
}

func newNativeClient(t *testing.T, server *httptest.Server) *provider.KimiWebProvider {
	t.Helper()
	client, err := provider.NewKimiWeb(provider.KimiWebOptions{BaseURL: server.URL, Token: "test-token", AgentID: "worker-1", Workspace: t.TempDir(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func newNativeServer(t *testing.T, response string, pending bool, waitFailures ...int) *httptest.Server {
	t.Helper()
	workspace := ""
	remainingWaitFailures := 0
	if len(waitFailures) > 0 {
		remainingWaitFailures = waitFailures[0]
	}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writeNative := func(value any) { _ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": value}) }
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/sessions":
			var body struct {
				WorkspaceRoot string `json:"workspace_root"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			workspace = body.WorkspaceRoot
			writeNative(map[string]any{"id": "session-1", "workspace_root": workspace})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/prompts"):
			writeNative(map[string]any{"prompt_id": "task-1", "status": "running"})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":wait"):
			if remainingWaitFailures > 0 {
				remainingWaitFailures--
				writer.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(writer).Encode(map[string]any{"code": 50000, "msg": "temporary failure"})
				return
			}
			interactions := []any{}
			if pending {
				interactions = append(interactions, map[string]any{"kind": "approval"})
			}
			writeNative(map[string]any{"timed_out": false, "pending_interactions": interactions, "session": map[string]any{"activity": "idle", "last_turn_outcome": "completed"}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/agents/main/turns"):
			writeNative(map[string]any{"turns": []any{map[string]any{"status": "ended", "items": []any{map[string]any{"type": "user_message", "prompt_ids": []string{"task-1"}}, map[string]any{"type": "assistant_message", "text": response}}}}})
		default:
			t.Errorf("unexpected native request %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
}
