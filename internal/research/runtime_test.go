package research_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/research"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

func TestChiefDelegatesToWorkersAndAggregatesFindings(t *testing.T) {
	cfg := loadConfig(t)
	runtime, err := research.New(cfg, research.DemoFactory())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runtime.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	status := runtime.Status()
	if status.State != research.StateCompleted {
		t.Fatalf("state = %s", status.State)
	}
	tasks := runtime.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("tasks = %#v", tasks)
	}
	for _, task := range tasks {
		if task.State != scheduler.StateCompleted {
			t.Fatalf("task = %#v", task)
		}
	}
	findings := runtime.Findings()
	if len(findings) < 3 {
		t.Fatalf("findings = %#v", findings)
	}
	var criticism bool
	for _, finding := range findings {
		if finding.Kind == "criticism" {
			criticism = true
		}
	}
	if !criticism {
		t.Fatal("independent criticism is missing")
	}
	if !strings.Contains(runtime.Report(), "research-a") || !strings.Contains(runtime.Report(), "Criticism") {
		t.Fatalf("report = %q", runtime.Report())
	}
	if status.Budget.Confirmed.Requests < 6 || status.Budget.Reserved.Requests != 0 {
		t.Fatalf("budget = %#v", status.Budget)
	}
	events := runtime.Events()
	for index, event := range events {
		if event.Sequence != uint64(index+1) {
			t.Fatalf("event sequence at %d = %d", index, event.Sequence)
		}
	}
}

func TestRuntimeCancellationStopsChiefAndWorkers(t *testing.T) {
	cfg := loadConfig(t)
	factory := func(_ string, agentConfig config.AgentConfig) (provider.Provider, error) {
		return provider.NewMockHandler(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
			if agentConfig.Role == "chief-scientist" {
				<-ctx.Done()
				return provider.Response{}, ctx.Err()
			}
			return provider.Response{Content: "unused"}, nil
		}), nil
	}
	runtime, err := research.New(cfg, factory)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := runtime.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
	if runtime.Status().State != research.StateCancelled {
		t.Fatalf("state = %s", runtime.Status().State)
	}
}

func TestRuntimeRejectsRealProviderWithoutPricing(t *testing.T) {
	cfg := loadConfig(t)
	cfg.Chief.Model.Provider = "openai"
	cfg.Chief.Model.APIKeyEnv = "TEST_UNUSED_KEY"
	_, err := research.New(cfg, func(string, config.AgentConfig) (provider.Provider, error) {
		return provider.NewMock(), nil
	})
	if err == nil || !strings.Contains(err.Error(), "pricing") {
		t.Fatalf("error = %v", err)
	}
}

func loadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load("../../configs/autoresearch.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
