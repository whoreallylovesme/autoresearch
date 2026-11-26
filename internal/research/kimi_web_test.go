package research_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestNativeKimiFactoryUsesPrivateTokenAndIndependentWorkspaces(t *testing.T) {
	root := t.TempDir()
	tokenPath := filepath.Join(root, "server.token")
	if err := os.WriteFile(tokenPath, []byte("native-test-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	configuration := loadConfig(t)
	model := &configuration.Agents[0].Model
	model.Provider = "kimi-web"
	model.BaseURL = "http://127.0.0.1:58627"
	model.Model = "kimi-code/kimi-for-coding"
	model.TokenFile = tokenPath
	model.SessionWorkspace = root
	model.Pricing = nil
	model.Capabilities.Tools = false
	model.Capabilities.Streaming = false
	model.Capabilities.StructuredOutputs = false
	model.Capabilities.SupportedParameters = nil
	model.Parameters = map[string]any{"permission_mode": "manual"}
	factory := research.DefaultFactory()
	worker, err := factory("worker-1", configuration.Agents[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := worker.(*provider.KimiWebProvider); !ok {
		t.Fatalf("native provider type = %T", worker)
	}
	if worker.Capabilities().Tools {
		t.Fatal("configured tools: false was overridden by the native provider")
	}
	for _, name := range worker.Capabilities().SupportedParameters {
		if name == "max_output_tokens" {
			t.Fatal("native provider advertised an unsupported hosted-API parameter")
		}
	}
	if _, err := research.New(configuration, factory); err != nil {
		t.Fatalf("subscription provider must not require token pricing: %v", err)
	}
	if err := os.Chmod(tokenPath, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = factory("worker-2", configuration.Agents[0])
	if err == nil || !strings.Contains(err.Error(), "permissions") || strings.Contains(err.Error(), "native-test-secret") {
		t.Fatalf("insecure token error = %v", err)
	}
}

func TestOpenAIChiefWithNativeKimiWorkerSessions(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "openai-test-secret")
	chief := newNativeMixedChief(t)
	defer chief.Close()
	var mu sync.Mutex
	workspaceRoots := map[string]bool{}
	workerCalls := 0
	nextSession := 0
	lastPrompts := map[string]string{}
	babyReady := make(chan struct{})
	babyArrivals := 0
	workers := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer native-test-secret" {
			t.Error("incorrect native transport credential")
		}
		mu.Lock()
		defer mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		respond := func(data any) { _ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": data}) }
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/sessions":
			nextSession++
			var body struct {
				WorkspaceRoot string `json:"workspace_root"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			workspaceRoots[body.WorkspaceRoot] = true
			respond(map[string]any{"id": fmt.Sprintf("native-%d", nextSession), "workspace_root": body.WorkspaceRoot})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/prompts"):
			workerCalls++
			var body struct {
				PromptID string `json:"prompt_id"`
				Content  []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			sessionID := path.Base(strings.TrimSuffix(request.URL.Path, "/prompts"))
			lastPrompts[sessionID] = body.PromptID
			if !strings.Contains(body.Content[0].Text, "submit_finding") && !strings.Contains(body.Content[0].Text, "submit_criticism") {
				t.Errorf("runtime tools not provided to native worker: %q", body.Content[0].Text)
			}
			if strings.Contains(body.Content[0].Text, "Investigate native") {
				babyArrivals++
				if babyArrivals == 2 {
					close(babyReady)
				}
				arrival := babyArrivals
				mu.Unlock()
				if arrival <= 2 {
					select {
					case <-babyReady:
					case <-request.Context().Done():
					}
				} else {
					t.Error("unexpected additional baby prompt")
				}
				mu.Lock()
			}
			respond(map[string]any{"prompt_id": body.PromptID, "status": "running"})
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, ":wait"):
			respond(map[string]any{"timed_out": false, "pending_interactions": []any{}, "session": map[string]any{"activity": "idle", "last_turn_outcome": "completed"}})
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/agents/main/turns"):
			sessionID := path.Base(strings.TrimSuffix(request.URL.Path, "/agents/main/turns"))
			respond(map[string]any{"turns": []any{map[string]any{"status": "ended", "items": []any{map[string]any{"type": "user_message", "prompt_ids": []string{lastPrompts[sessionID]}}, map[string]any{"type": "assistant_message", "text": `{"final_response":"Native finding from ` + sessionID + `"}`}}}}})
		default:
			t.Errorf("unexpected native request %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer workers.Close()
	configuration := mixedProviderConfig(t, chief.URL, workers.URL)
	tokenPath := filepath.Join(t.TempDir(), "server.token")
	if err := os.WriteFile(tokenPath, []byte("native-test-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := t.TempDir()
	for index := range configuration.Agents {
		model := &configuration.Agents[index].Model
		model.Provider = "kimi-web"
		model.BaseURL = workers.URL
		model.BaseURLEnv = ""
		model.APIKeyEnv = ""
		model.TokenFile = tokenPath
		model.SessionWorkspace = workspaceRoot
		model.Pricing = nil
		model.Capabilities.Streaming = false
		model.Capabilities.StructuredOutputs = false
		model.Capabilities.SupportedParameters = []string{"permission_mode"}
		model.Parameters = map[string]any{"permission_mode": "manual"}
	}
	runtime, err := research.New(configuration, research.DefaultFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if runtime.Status().State != research.StateCompleted || workerCalls != 3 || len(workspaceRoots) != 3 {
		t.Fatalf("state = %s, native calls = %d, separate workspaces = %d", runtime.Status().State, workerCalls, len(workspaceRoots))
	}
}

func newNativeMixedChief(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body provider.Request
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		steps := 0
		for _, message := range body.Messages {
			if message.Role == "assistant" && len(message.ToolCalls) > 0 {
				steps++
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		if steps >= 5 {
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": "chief-final", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Native mixed-fleet report"}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}})
			return
		}
		var call provider.ToolCall
		switch steps {
		case 0:
			call = provider.ToolCall{ID: "chief-a", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"native-a","title":"Native A","description":"Investigate native A","role":"baby-scientist","idempotency_key":"native-a"}`)}
		case 1:
			call = provider.ToolCall{ID: "chief-b", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"native-b","title":"Native B","description":"Investigate native B","role":"baby-scientist","idempotency_key":"native-b"}`)}
		case 2:
			call = provider.ToolCall{ID: "chief-c", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"native-critic","title":"Native criticism","description":"Review native findings","role":"critic","dependencies":["native-a","native-b"],"idempotency_key":"native-critic"}`)}
		case 3:
			call = provider.ToolCall{ID: "chief-query", Name: "query_knowledge", Arguments: json.RawMessage(`{"query":"all findings","wait_for_tasks":true}`)}
		case 4:
			call = provider.ToolCall{ID: "chief-finalize", Name: "finalize_research", Arguments: json.RawMessage(`{}`)}
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"id": fmt.Sprintf("chief-%d", steps), "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}}}}}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5}})
	}))
}
