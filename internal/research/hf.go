package research

import (
	"context"
	"errors"
	"strings"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

type HFMockProvider struct {
	local         provider.Provider
	agentID       string
	configuration config.AgentConfig
}

func NewHFMockProvider(local provider.Provider, agentID string, configuration config.AgentConfig) *HFMockProvider {
	return &HFMockProvider{local: local, agentID: agentID, configuration: configuration}
}

func (p *HFMockProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	if p.local == nil {
		return provider.Response{}, errors.New("local Hugging Face provider is not configured")
	}
	if strings.Contains(strings.ToLower(demoUserText(request)), "operator message") {
		return p.narrate(ctx, request, true)
	}
	var scripted provider.Response
	var err error
	if p.configuration.Role == "chief-scientist" {
		scripted, err = demoChief(ctx, request)
	} else {
		scripted, err = demoWorker(p.agentID, p.configuration, request)
	}
	if err != nil {
		return provider.Response{}, err
	}
	if len(scripted.ToolCalls) > 0 {
		return scripted, nil
	}
	return p.narrate(ctx, request, false)
}

func (p *HFMockProvider) Stream(context.Context, provider.Request) (<-chan provider.Event, error) {
	return nil, errors.New("HFMockProvider streaming is unavailable; use Generate")
}

func (p *HFMockProvider) Capabilities() provider.Capabilities {
	capabilities := p.local.Capabilities()
	capabilities.Tools = true
	capabilities.StructuredOutputs = true
	return capabilities
}

func (p *HFMockProvider) narrate(ctx context.Context, request provider.Request, operator bool) (provider.Response, error) {
	instruction := "You are a terse runtime narrator. Restate only the supplied deterministic task facts. Do not add claims, sources, citations, URLs, examples, headings, lists, or reasoning. Use at most 30 words."
	if operator {
		instruction = "Answer the operator using only the supplied research state. Do not invent sources, citations, metrics, reasoning traces, headings, or lists. Use at most 50 words."
	}
	user := lastUserContent(request.Messages)
	if len(user) > 2000 {
		user = user[:2000]
	}
	request.Messages = []provider.Message{{Role: "system", Content: instruction}, {Role: "user", Content: user}}
	request.Tools = nil
	request.Parameters = map[string]any{"temperature": 0, "max_output_tokens": 64}
	response, err := p.local.Generate(ctx, request)
	if err != nil {
		return provider.Response{}, err
	}
	response.Content = sanitizeNarration(response.Content)
	return response, nil
}

func sanitizeNarration(raw string) string {
	text := strings.Join(strings.Fields(raw), " ")
	lower := strings.ToLower(text)
	blocked := []string{"http", "arxiv", "doi", "google scholar", "book", "source", "reference", "journal", "proceedings", "starnes", "isbn", "###", "citation"}
	if text == "" || len(text) > 500 {
		return "Local model narration was rejected; no evidence is claimed."
	}
	for _, marker := range blocked {
		if strings.Contains(lower, marker) {
			return "Local model narration was rejected; no evidence is claimed."
		}
	}
	return text
}

func lastUserContent(messages []provider.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messages[index].Content
		}
	}
	return "Provide a bounded operational narration; no evidence is claimed."
}
