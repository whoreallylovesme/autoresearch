package provider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func TestMockReturnsScriptedResponsesAndRecordsRequests(t *testing.T) {
	mock := provider.NewMock(provider.Response{Content: "first"}, provider.Response{Content: "second"})
	request := provider.Request{Model: "mock", Messages: []provider.Message{{Role: "user", Content: "hello"}}}
	first, err := mock.Generate(context.Background(), request)
	if err != nil || first.Content != "first" {
		t.Fatalf("first Generate() = %#v, %v", first, err)
	}
	second, err := mock.Generate(context.Background(), request)
	if err != nil || second.Content != "second" {
		t.Fatalf("second Generate() = %#v, %v", second, err)
	}
	if len(mock.Requests()) != 2 {
		t.Fatalf("recorded requests = %d", len(mock.Requests()))
	}
	if _, err := mock.Generate(context.Background(), request); err == nil {
		t.Fatal("exhausted mock returned no error")
	}
}

func TestMockHandlerSupportsDeterministicBehavior(t *testing.T) {
	mock := provider.NewMockHandler(func(_ context.Context, request provider.Request) (provider.Response, error) {
		return provider.Response{Content: request.Messages[len(request.Messages)-1].Content}, nil
	})
	response, err := mock.Generate(context.Background(), provider.Request{Messages: []provider.Message{{Role: "user", Content: "deterministic"}}})
	if err != nil || response.Content != "deterministic" {
		t.Fatalf("Generate() = %#v, %v", response, err)
	}
}

func TestOpenAIGenerateMapsToolsParametersAndUsage(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("authorization header missing")
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"id":"chat-1","model":"test-model","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"create_task","arguments":"{\"title\":\"Investigate\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "test-secret")
	response, err := client.Generate(context.Background(), provider.Request{
		Messages:   []provider.Message{{Role: "user", Content: "research"}},
		Tools:      []provider.ToolDefinition{{Name: "create_task", Description: "Create work", Parameters: json.RawMessage(`{"type":"object"}`)}},
		Parameters: map[string]any{"max_output_tokens": 128},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.ID != "chat-1" || len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "create_task" {
		t.Fatalf("response = %#v", response)
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 7 || response.Usage.CachedTokens != 4 || response.Usage.ReasoningTokens != 3 {
		t.Fatalf("usage = %#v", response.Usage)
	}
	if received["max_completion_tokens"] != float64(128) {
		t.Fatalf("max_completion_tokens = %#v; body = %#v", received["max_completion_tokens"], received)
	}
	if _, exists := received["max_output_tokens"]; exists {
		t.Fatal("internal parameter leaked to wire")
	}
	if len(received["tools"].([]any)) != 1 {
		t.Fatalf("tools = %#v", received["tools"])
	}
}

func TestOpenAIRejectsUnsupportedParameterBeforeRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL, "secret")
	_, err := client.Generate(context.Background(), provider.Request{Parameters: map[string]any{"unknown": true}})
	if err == nil || !strings.Contains(err.Error(), "unknown") || calls.Load() != 0 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
}

func TestOpenAIRetriesThrottling(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(writer, `{"error":{"message":"slow down"}}`)
			return
		}
		fmt.Fprint(writer, `{"id":"ok","choices":[{"message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "secret")
	response, err := client.Generate(context.Background(), provider.Request{})
	if err != nil || response.Content != "done" || calls.Load() != 2 {
		t.Fatalf("Generate() = %#v, %v, calls = %d", response, err, calls.Load())
	}
}

func TestOpenAIDoesNotRetryAuthenticationFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "secret")
	_, err := client.Generate(context.Background(), provider.Request{})
	var providerError *provider.Error
	if !errors.As(err, &providerError) || providerError.Kind != provider.ErrorAuthentication || calls.Load() != 1 {
		t.Fatalf("error = %#v, calls = %d", err, calls.Load())
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked credentials: %v", err)
	}
}

func TestOpenAIHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		select {
		case <-request.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := client.Generate(ctx, provider.Request{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenAIStreamsContentAndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, "data: {\"id\":\"stream\",\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		fmt.Fprint(writer, "data: {\"id\":\"stream\",\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(writer, "data: {\"id\":\"stream\",\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1}}\n\n")
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, "secret")
	events, err := client.Stream(context.Background(), provider.Request{})
	if err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	var final provider.Response
	for event := range events {
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		content.WriteString(event.Delta)
		if event.Type == provider.EventDone {
			final = event.Response
		}
	}
	if content.String() != "hello" || final.Usage.InputTokens != 2 {
		t.Fatalf("content = %q, final = %#v", content.String(), final)
	}
}

func newTestClient(t *testing.T, baseURL, secret string) *provider.OpenAIProvider {
	t.Helper()
	client, err := provider.NewOpenAI(provider.OpenAIOptions{
		Provider:          "openai",
		Model:             "test-model",
		BaseURL:           baseURL + "/v1",
		APIKey:            secret,
		RequestsPerMinute: 1000000,
		Capabilities: provider.Capabilities{
			Streaming:           true,
			Tools:               true,
			SupportedParameters: []string{"max_output_tokens"},
		},
		Retry: provider.RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestOpenAISharedLimiterCoordinatesProviderInstances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"id":"ok","choices":[{"message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer server.Close()
	limiter := provider.NewRateLimiter(6000)
	newClient := func() *provider.OpenAIProvider {
		client, err := provider.NewOpenAI(provider.OpenAIOptions{Provider: "openai", Model: "test", BaseURL: server.URL + "/v1", Capabilities: provider.Capabilities{SupportedParameters: []string{"max_output_tokens"}}, Retry: provider.RetryPolicy{MaxAttempts: 1}, Limiter: limiter})
		if err != nil {
			t.Fatal(err)
		}
		return client
	}
	first := newClient()
	second := newClient()
	start := time.Now()
	if _, err := first.Generate(context.Background(), provider.Request{}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Generate(context.Background(), provider.Request{}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 7*time.Millisecond {
		t.Fatalf("shared limiter did not coordinate requests: %s", elapsed)
	}
}
