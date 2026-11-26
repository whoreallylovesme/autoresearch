package provider_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func TestKimiMapsReasoningJSONModeAndUsage(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing Kimi authorization")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Write([]byte(`{"id":"kimi-1","model":"kimi-k3.1-pro","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"{\"status\":\"ok\"}"}}],"usage":{"prompt_tokens":20,"completion_tokens":9,"prompt_tokens_details":{"cache_write_tokens":256},"completion_tokens_details":{"reasoning_tokens":7}}}`))
	}))
	defer server.Close()
	client, err := provider.NewKimi(provider.OpenAIOptions{
		Model: "kimi-k3.1-pro", BaseURL: server.URL + "/v1", APIKey: "test-secret", HTTPClient: server.Client(),
		Capabilities: provider.Capabilities{Streaming: true, Tools: true, StructuredOutputs: true, SupportedParameters: []string{"max_output_tokens", "reasoning_effort", "response_format"}, ReasoningEfforts: []string{"low", "high", "max"}},
		Retry:        provider.RetryPolicy{MaxAttempts: 1, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Generate(context.Background(), provider.Request{Parameters: map[string]any{"max_output_tokens": 128, "reasoning_effort": "high", "response_format": "json_object"}})
	if err != nil {
		t.Fatal(err)
	}
	if received["model"] != "kimi-k3.1-pro" || received["max_tokens"] != float64(128) || received["reasoning_effort"] != "high" {
		t.Fatalf("request = %#v", received)
	}
	if _, exists := received["max_completion_tokens"]; exists {
		t.Fatalf("OpenAI-only token field leaked to Kimi: %#v", received)
	}
	if received["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("response_format = %#v", received["response_format"])
	}
	if response.Usage.ReasoningTokens != 7 || response.Usage.CacheWriteTokens != 256 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestKimiRejectsUnsupportedReasoningEffort(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := provider.NewKimi(provider.OpenAIOptions{Model: "kimi-k3.1-pro", BaseURL: server.URL + "/v1", APIKey: "test", Capabilities: provider.Capabilities{Tools: true, StructuredOutputs: true, SupportedParameters: []string{"reasoning_effort"}, ReasoningEfforts: []string{"low", "high", "max"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), provider.Request{Parameters: map[string]any{"reasoning_effort": "medium"}})
	if err == nil || !strings.Contains(err.Error(), "reasoning_effort") || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestKimiJSONModeRequiresStructuredOutputCapability(t *testing.T) {
	client, err := provider.NewKimi(provider.OpenAIOptions{Model: "kimi-k3.1-pro", BaseURL: "http://127.0.0.1:1/v1", APIKey: "test", Capabilities: provider.Capabilities{StructuredOutputs: false, SupportedParameters: []string{"response_format"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background(), provider.Request{Parameters: map[string]any{"response_format": "json_object"}})
	if err == nil || !strings.Contains(err.Error(), "structured output") {
		t.Fatalf("error = %v", err)
	}
}
