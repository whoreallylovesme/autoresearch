package research

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

func DemoFactory() ProviderFactory {
	base := demoFactory()
	return func(agentID string, configuration config.AgentConfig) (provider.Provider, error) {
		model, err := base(agentID, configuration)
		if err != nil {
			return nil, err
		}
		rawDelay := os.Getenv("AUTORESEARCH_DEMO_DELAY")
		if rawDelay == "" {
			return model, nil
		}
		delay, err := time.ParseDuration(rawDelay)
		if err != nil || delay < 0 {
			return nil, fmt.Errorf("invalid AUTORESEARCH_DEMO_DELAY %q", rawDelay)
		}
		return provider.NewMockHandler(func(ctx context.Context, request provider.Request) (provider.Response, error) {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
				return model.Generate(ctx, request)
			case <-ctx.Done():
				return provider.Response{}, ctx.Err()
			}
		}), nil
	}
}

func demoFactory() ProviderFactory {
	return func(agentID string, configuration config.AgentConfig) (provider.Provider, error) {
		if configuration.Role == "chief-scientist" {
			return provider.NewMockHandler(demoChief), nil
		}
		return provider.NewMockHandler(func(_ context.Context, _ provider.Request) (provider.Response, error) {
			content := fmt.Sprintf("Finding from %s: deterministic evidence about explicit tasks and dependencies", agentID)
			switch configuration.Role {
			case "critic":
				content = "Criticism: deterministic evidence reviewed; no experimental verification is claimed"
			case "brainstormer":
				content = "Speculation: alternative scheduling directions require independent evaluation"
			case "developer":
				content = "Implementation note: deterministic task state is represented without executing an experiment"
			}
			return provider.Response{Content: content, Usage: provider.Usage{InputTokens: 5, OutputTokens: 3}}, nil
		}), nil
	}
}

func demoChief(_ context.Context, request provider.Request) (provider.Response, error) {
	steps := 0
	for _, message := range request.Messages {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			steps++
		}
	}
	switch steps {
	case 0:
		return demoToolCall("delegate_task", `{"task_id":"research-a","title":"Research A","description":"Investigate deterministic scheduling","role":"baby-scientist","priority":10,"idempotency_key":"chief-research-a"}`), nil
	case 1:
		return demoToolCall("delegate_task", `{"task_id":"research-b","title":"Research B","description":"Investigate explicit dependencies","role":"baby-scientist","priority":9,"idempotency_key":"chief-research-b"}`), nil
	case 2:
		return demoToolCall("delegate_task", `{"task_id":"critic-review","title":"Independent review","description":"Review both findings and unsupported claims","role":"critic","dependencies":["research-a","research-b"],"idempotency_key":"chief-critic"}`), nil
	case 3:
		return demoToolCall("query_knowledge", `{"query":"all findings","wait_for_tasks":true}`), nil
	case 4:
		return demoToolCall("finalize_research", `{}`), nil
	default:
		return provider.Response{Content: "Final report ready", Usage: provider.Usage{InputTokens: 10, OutputTokens: 4}}, nil
	}
}

func demoToolCall(name, arguments string) provider.Response {
	return provider.Response{ToolCalls: []provider.ToolCall{{ID: "call-" + name, Name: name, Arguments: json.RawMessage(arguments)}}}
}
