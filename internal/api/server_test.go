package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/api"
	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestAPIStartsAndExposesResearchRun(t *testing.T) {
	runtime := newRuntime(t, research.DemoFactory())
	server := httptest.NewServer(api.New(runtime))
	defer server.Close()
	response := request(t, http.MethodPost, server.URL+"/api/v1/run/start", nil)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", response.StatusCode)
	}
	response.Body.Close()
	waitForState(t, server.URL, research.StateCompleted)
	for _, path := range []string{"/api/v1/run/status", "/api/v1/agents", "/api/v1/tasks", "/api/v1/budget", "/api/v1/report", "/api/v1/graph", "/api/v1/papers", "/api/v1/experiments", "/api/v1/playback", "/metrics", "/api/v1/events?after=2"} {
		response = request(t, http.MethodGet, server.URL+path, nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", path, response.StatusCode)
		}
		response.Body.Close()
	}
	response = request(t, http.MethodGet, server.URL+"/api/v1/tasks", nil)
	var taskPayload struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&taskPayload); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	foundResearchA := false
	for _, task := range taskPayload.Tasks {
		if task["id"] == "research-a" {
			foundResearchA = true
		}
	}
	if !foundResearchA {
		t.Fatalf("task JSON contract = %#v", taskPayload)
	}
	response = request(t, http.MethodGet, server.URL+"/api/v1/report", nil)
	var report map[string]string
	if err := json.NewDecoder(response.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if !strings.Contains(report["report"], "Research Report") {
		t.Fatalf("report = %#v", report)
	}
	response = request(t, http.MethodGet, server.URL+"/api/v1/events/stream", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	var eventLine string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			eventLine = scanner.Text()
			break
		}
	}
	response.Body.Close()
	if !strings.Contains(eventLine, "research.created") {
		t.Fatalf("first SSE event = %q", eventLine)
	}
}

func TestAPIStopCancelsActiveRun(t *testing.T) {
	factory := func(_ string, configuration config.AgentConfig) (provider.Provider, error) {
		return provider.NewMockHandler(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
			if configuration.Role == "chief-scientist" {
				<-ctx.Done()
				return provider.Response{}, ctx.Err()
			}
			return provider.Response{Content: "unused"}, nil
		}), nil
	}
	runtime := newRuntime(t, factory)
	server := httptest.NewServer(api.New(runtime))
	defer server.Close()
	response := request(t, http.MethodPost, server.URL+"/api/v1/run/start", nil)
	response.Body.Close()
	response = request(t, http.MethodPost, server.URL+"/api/v1/run/stop", nil)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("stop status = %d", response.StatusCode)
	}
	response.Body.Close()
	waitForState(t, server.URL, research.StateCancelled)
}

func TestAPIRejectsWrongMethod(t *testing.T) {
	server := httptest.NewServer(api.New(newRuntime(t, research.DemoFactory())))
	defer server.Close()
	response := request(t, http.MethodDelete, server.URL+"/api/v1/run/status", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func newRuntime(t *testing.T, factory research.ProviderFactory) *research.Runtime {
	t.Helper()
	cfg, err := config.Load("../../configs/autoresearch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := research.New(cfg, factory)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func waitForState(t *testing.T, baseURL string, expected research.State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := request(t, http.MethodGet, baseURL+"/api/v1/run/status", nil)
		var status research.Status
		if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if status.State == expected {
			return
		}
		if status.State == research.StateFailed {
			t.Fatalf("run failed while waiting for %s", expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("run did not reach %s", expected)
}

func request(t *testing.T, method, url string, body []byte) *http.Response {
	t.Helper()
	response, err := http.DefaultClient.Do(mustRequest(t, method, url, body))
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func mustRequest(t *testing.T, method, url string, body []byte) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestAPIPauseResumeFinalizeAndChat(t *testing.T) {
	t.Setenv("AUTORESEARCH_DEMO_DELAY", "30ms")
	runtime := newRuntime(t, research.DemoFactory())
	server := httptest.NewServer(api.New(runtime))
	defer server.Close()
	response := request(t, http.MethodPost, server.URL+"/api/v1/run/start", nil)
	response.Body.Close()
	response = request(t, http.MethodPost, server.URL+"/api/v1/run/pause", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d", response.StatusCode)
	}
	response.Body.Close()
	waitForState(t, server.URL, research.StatePaused)
	response = request(t, http.MethodPost, server.URL+"/api/v1/run/resume", nil)
	response.Body.Close()
	waitForState(t, server.URL, research.StateCompleted)
	response = request(t, http.MethodPost, server.URL+"/api/v1/run/finalize", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("finalize status = %d", response.StatusCode)
	}
	response.Body.Close()
	response = request(t, http.MethodPost, server.URL+"/api/v1/chat", []byte(`{"message":"Explain status","directive":false}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d", response.StatusCode)
	}
	var chat map[string]string
	if err := json.NewDecoder(response.Body).Decode(&chat); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if chat["response"] == "" {
		t.Fatalf("chat = %#v", chat)
	}
	response = request(t, http.MethodGet, server.URL+"/api/v1/messages", nil)
	var messages map[string][]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&messages); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(messages["messages"]) < 2 {
		t.Fatalf("messages = %#v", messages)
	}
}
