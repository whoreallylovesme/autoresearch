package research

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
		return provider.NewMockHandler(func(_ context.Context, request provider.Request) (provider.Response, error) {
			return demoWorker(agentID, configuration, request)
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
	if strings.Contains(strings.ToLower(demoUserText(request)), "literature review") {
		return demoLiteratureChief(steps), nil
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

func demoLiteratureChief(steps int) provider.Response {
	switch steps {
	case 0:
		return demoToolCall("register_hypothesis", `{"id":"hypothesis-multi-agent-coordination","description":"Structured multi-agent coordination may improve reproducible scientific task decomposition; this remains unverified."}`)
	case 1:
		return demoToolCall("delegate_task", `{"task_id":"paper-autogen","title":"AutoGen literature investigation","description":"Read AutoGen bibliographic metadata and record a cautious interpretation with citation","role":"baby-scientist","priority":10,"idempotency_key":"chief-literature-autogen"}`)
	case 2:
		return demoToolCall("delegate_task", `{"task_id":"paper-camel","title":"CAMEL literature investigation","description":"Read CAMEL bibliographic metadata and record a cautious interpretation with citation","role":"baby-scientist","priority":9,"idempotency_key":"chief-literature-camel"}`)
	case 3:
		return demoToolCall("delegate_task", `{"task_id":"literature-critic","title":"Independent citation review","description":"Review AutoGen and CAMEL findings for citation accuracy and unsupported claims","role":"critic","dependencies":["paper-autogen","paper-camel"],"idempotency_key":"chief-literature-critic"}`)
	case 4:
		return demoToolCall("query_knowledge", `{"query":"all findings","wait_for_tasks":true}`)
	case 5:
		return demoToolCall("finalize_research", `{}`)
	default:
		return provider.Response{Content: "Cited literature review ready", Usage: provider.Usage{InputTokens: 10, OutputTokens: 4}}
	}
}

func demoWorker(agentID string, configuration config.AgentConfig, request provider.Request) (provider.Response, error) {
	text := strings.ToLower(demoUserText(request))
	if configuration.Role == "baby-scientist" && (strings.Contains(text, "autogen") || strings.Contains(text, "camel")) {
		steps := 0
		paperID := ""
		for _, message := range request.Messages {
			if message.Role == "assistant" && len(message.ToolCalls) > 0 {
				steps++
			}
			if message.Role == "tool" && strings.Contains(message.Content, "papers") {
				var payload struct {
					Papers []struct {
						ID string `json:"id"`
					} `json:"papers"`
				}
				if json.Unmarshal([]byte(message.Content), &payload) == nil && len(payload.Papers) > 0 {
					paperID = payload.Papers[0].ID
				}
			}
		}
		query := "AutoGen"
		if strings.Contains(text, "camel") {
			query = "CAMEL"
		}
		switch steps {
		case 0:
			arguments, _ := json.Marshal(map[string]any{"query": query, "limit": 1})
			return demoToolCall("search_papers", string(arguments)), nil
		case 1:
			if paperID == "" {
				return provider.Response{}, fmt.Errorf("paper search returned no paper ID")
			}
			taskID := demoTaskID(request)
			summary := fmt.Sprintf("Metadata-level interpretation of %s: structured agent interaction is a candidate coordination mechanism; full text was not reviewed and no effectiveness claim is verified.", query)
			arguments, _ := json.Marshal(map[string]any{"task_id": taskID, "paper_id": paperID, "claim_type": "interpretation", "section": "Abstract", "summary": summary, "evidence": []string{"bundled bibliographic metadata"}})
			return demoToolCall("submit_finding", string(arguments)), nil
		default:
			return provider.Response{Content: "Cited metadata-level interpretation recorded; full text was not reviewed", Usage: provider.Usage{InputTokens: 5, OutputTokens: 3}}, nil
		}
	}
	content := fmt.Sprintf("Finding from %s: deterministic evidence about explicit tasks and dependencies", agentID)
	switch configuration.Role {
	case "critic":
		content = "Criticism: deterministic evidence reviewed; no experimental verification is claimed"
		if strings.Contains(text, "literature-critic") {
			content = "Criticism: citations resolve to bundled arXiv metadata; no full-text review, effectiveness conclusion, or experimental verification is claimed"
		}
	case "brainstormer":
		content = "Speculation: alternative scheduling directions require independent evaluation"
	case "developer":
		content = "Implementation note: deterministic task state is represented without executing an experiment"
	}
	return provider.Response{Content: content, Usage: provider.Usage{InputTokens: 5, OutputTokens: 3}}, nil
}

func demoUserText(request provider.Request) string {
	var text strings.Builder
	for _, message := range request.Messages {
		if message.Role == "user" {
			text.WriteString(message.Content)
			text.WriteString("\n")
		}
	}
	return text.String()
}

func demoTaskID(request provider.Request) string {
	text := demoUserText(request)
	marker := "Task ID: "
	index := strings.LastIndex(text, marker)
	if index < 0 {
		return ""
	}
	remainder := text[index+len(marker):]
	return strings.Fields(remainder)[0]
}
