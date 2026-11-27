package research_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

type scriptedChiefConfig struct {
	credential    string
	calls         *atomic.Int32
	idPrefix      string
	finalID       string
	finalResponse string
	script        []provider.ToolCall
}

func newScriptedChief(t *testing.T, config scriptedChiefConfig) *httptest.Server {
	t.Helper()
	if len(config.script) != 5 {
		t.Fatalf("scripted chief requires five tool calls, got %d", len(config.script))
	}
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if config.credential != "" && request.Header.Get("Authorization") != "Bearer "+config.credential {
			t.Error("incorrect OpenAI credential")
			http.Error(writer, "incorrect OpenAI credential", http.StatusUnauthorized)
			return
		}
		if config.calls != nil {
			config.calls.Add(1)
		}
		var body provider.Request
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, "malformed OpenAI chief request: "+err.Error(), http.StatusBadRequest)
			return
		}
		steps := 0
		for _, message := range body.Messages {
			if message.Role == "assistant" && len(message.ToolCalls) > 0 {
				steps++
			}
		}
		if steps > len(config.script) {
			http.Error(writer, fmt.Sprintf("unexpected OpenAI chief step %d", steps), http.StatusBadRequest)
			return
		}

		writer.Header().Set("Content-Type", "application/json")
		usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 5}
		var response map[string]any
		if steps == len(config.script) {
			response = map[string]any{"id": config.finalID, "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": config.finalResponse}}}, "usage": usage}
		} else {
			call := config.script[steps]
			response = map[string]any{"id": fmt.Sprintf("%s-%d", config.idPrefix, steps), "choices": []any{map[string]any{"finish_reason": "tool_calls", "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": string(call.Arguments)}}}}}}, "usage": usage}
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			t.Errorf("encode OpenAI chief response: %v", err)
		}
	}))
}

func TestScriptedChiefRejectsMalformedRequest(t *testing.T) {
	server := newScriptedChief(t, scriptedChiefConfig{script: make([]provider.ToolCall, 5)})
	defer server.Close()

	response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(`{"messages":`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed request status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}

func TestScriptedChiefRejectsUnexpectedStep(t *testing.T) {
	server := newScriptedChief(t, scriptedChiefConfig{script: make([]provider.ToolCall, 5)})
	defer server.Close()
	messages := make([]provider.Message, 6)
	for index := range messages {
		messages[index] = provider.Message{Role: "assistant", ToolCalls: []provider.ToolCall{{ID: "unexpected"}}}
	}
	body, err := json.Marshal(provider.Request{Messages: messages})
	if err != nil {
		t.Fatal(err)
	}

	response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected step status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
}
