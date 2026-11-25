package telemetry_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
	"github.com/whoreallylovesme/autoresearch/internal/telemetry"
)

func TestMetricsExposeRuntimeSnapshots(t *testing.T) {
	configuration, err := config.Load("../../configs/autoresearch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := research.New(configuration, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	telemetry.Metrics(ctx, response, runtime)
	body := response.Body.String()
	for _, expected := range []string{"autoresearch_tasks", "autoresearch_agents", "autoresearch_budget_confirmed_usd", "autoresearch_events_total", "state=\"completed\""} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("content type = %s", response.Header().Get("Content-Type"))
	}
}

func TestTraceparentUsesW3CFormat(t *testing.T) {
	trace := telemetry.NewTraceparent()
	if len(trace) != 55 || !strings.HasPrefix(trace, "00-") || !strings.HasSuffix(trace, "-01") {
		t.Fatalf("traceparent = %q", trace)
	}
}
