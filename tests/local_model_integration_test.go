//go:build localmodel

package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/research"
)

func TestLocalHFAutonomousResearchRun(t *testing.T) {
	response, err := http.Get("http://127.0.0.1:8082/health")
	if err != nil {
		t.Fatalf("local model health check failed: %v", err)
	}
	response.Body.Close()
	configuration, err := config.Load("../configs/local-hf.yaml")
	if err != nil {
		t.Fatal(err)
	}
	configuration.Runtime.DataDir = t.TempDir()
	runtime, err := research.NewPersistent(configuration, research.DefaultFactory())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("local HF research run failed: %v", err)
	}
	if runtime.Status().State != research.StateCompleted || len(runtime.Findings()) < 3 || len(runtime.Messages()) < 3 {
		t.Fatalf("unexpected research state: %#v, findings = %d", runtime.Status(), len(runtime.Findings()))
	}
	if !strings.Contains(runtime.Report(), "Research Report") {
		t.Fatalf("report = %q", runtime.Report())
	}
	chat, err := runtime.Chat(context.Background(), "Summarize the local verification run", false)
	if err != nil || strings.TrimSpace(chat) == "" {
		t.Fatalf("local HF chat = %q, %v", chat, err)
	}
}
