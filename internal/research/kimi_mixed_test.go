package research_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestOpenAIChiefWithSeparateKimiWorkers(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "openai-test-secret")
	t.Setenv("TEST_KIMI_KEY", "kimi-test-secret")
	var chiefCalls atomic.Int32
	var workerCalls atomic.Int32
	chiefServer := newScriptedChief(t, scriptedChiefConfig{
		credential:    "openai-test-secret",
		calls:         &chiefCalls,
		idPrefix:      "openai",
		finalID:       "openai-final",
		finalResponse: "OpenAI Chief Scientist final report",
		script: []provider.ToolCall{
			{ID: "openai-a", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"research-a","title":"Research A","description":"Investigate deterministic scheduling","role":"baby-scientist","priority":10,"idempotency_key":"chief-research-a"}`)},
			{ID: "openai-b", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"research-b","title":"Research B","description":"Investigate explicit dependencies","role":"baby-scientist","priority":9,"idempotency_key":"chief-research-b"}`)},
			{ID: "openai-critic", Name: "delegate_task", Arguments: json.RawMessage(`{"task_id":"critic-review","title":"Independent review","description":"Review both findings","role":"critic","dependencies":["research-a","research-b"],"idempotency_key":"chief-critic"}`)},
			{ID: "openai-query", Name: "query_knowledge", Arguments: json.RawMessage(`{"query":"all findings","wait_for_tasks":true}`)},
			{ID: "openai-finalize", Name: "finalize_research", Arguments: json.RawMessage(`{}`)},
		},
	})
	defer chiefServer.Close()
	workerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer kimi-test-secret" {
			t.Error("incorrect Kimi credential")
		}
		workerCalls.Add(1)
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "kimi-k3.1-pro" || body["reasoning_effort"] != "high" {
			t.Errorf("unexpected Kimi request: %#v", body)
		}
		if _, exists := body["max_completion_tokens"]; exists {
			t.Errorf("OpenAI-only token field reached Kimi: %#v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"id": "kimi-worker", "model": "kimi-k3.1-pro", "choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "Kimi worker finding with bounded evidence"}}}, "usage": map[string]any{"prompt_tokens": 8, "completion_tokens": 4, "prompt_tokens_details": map[string]any{"cache_write_tokens": 32}, "completion_tokens_details": map[string]any{"reasoning_tokens": 2}}})
	}))
	defer workerServer.Close()
	configuration := mixedProviderConfig(t, chiefServer.URL, workerServer.URL)
	factory := research.DefaultFactory()
	workerProvider, err := factory("worker-1", configuration.Agents[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := workerProvider.(*provider.KimiProvider); !ok {
		t.Fatalf("worker provider type = %T, want *provider.KimiProvider", workerProvider)
	}
	runtime, err := research.New(configuration, factory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("mixed-provider run failed: %v", err)
	}
	if runtime.Status().State != research.StateCompleted || len(runtime.Tasks()) != 3 {
		t.Fatalf("status = %#v", runtime.Status())
	}
	if workerCalls.Load() != 3 {
		t.Fatalf("Kimi worker calls = %d, want 3", workerCalls.Load())
	}
	if chiefCalls.Load() < 5 {
		t.Fatalf("OpenAI chief calls = %d", chiefCalls.Load())
	}
}

func mixedProviderConfig(t *testing.T, chiefURL, workerURL string) *config.Config {
	t.Helper()
	configuration := loadConfig(t)
	configuration.Chief.Model.Provider = "openai"
	configuration.Chief.Model.Model = "test-openai-chief"
	configuration.Chief.Model.BaseURL = chiefURL + "/v1"
	configuration.Chief.Model.APIKeyEnv = "TEST_OPENAI_KEY"
	configuration.Chief.Model.Parameters = map[string]any{"max_output_tokens": 128}
	configuration.Chief.Model.Pricing = &config.PricingConfig{}
	configuration.Chief.Model.Estimation.MaxInputTokens = 1024
	configuration.Chief.Model.Capabilities.Tools = true
	configuration.Chief.Model.Capabilities.StructuredOutputs = true
	for index := range configuration.Agents {
		model := &configuration.Agents[index].Model
		model.Provider = "kimi"
		model.Model = "kimi-k3.1-pro"
		model.BaseURL = workerURL + "/v1"
		model.APIKeyEnv = "TEST_KIMI_KEY"
		model.Parameters = map[string]any{"max_output_tokens": 128, "reasoning_effort": "high", "response_format": "text"}
		model.Pricing = &config.PricingConfig{}
		model.Estimation.MaxInputTokens = 1024
		model.Capabilities.Tools = true
		model.Capabilities.StructuredOutputs = true
		model.Capabilities.SupportedParameters = []string{"max_output_tokens", "reasoning_effort", "response_format"}
		model.Capabilities.ReasoningEfforts = []string{"low", "high", "max"}
	}
	return configuration
}
