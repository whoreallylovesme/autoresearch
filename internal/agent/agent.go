package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/whoreallylovesme/autoresearch/internal/config"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
)

var ErrIterationLimit = errors.New("agent iteration limit reached")

type ToolDispatcher interface {
	Definitions(agentID string) []provider.ToolDefinition
	Execute(ctx context.Context, agentID, name string, arguments json.RawMessage) (json.RawMessage, error)
}

type Assignment struct {
	RunID        string
	TaskID       string
	Objective    string
	Restrictions string
	Context      []provider.Message
}

type Result struct {
	Content     string
	Usage       provider.Usage
	Iterations  int
	Compactions int
}

type Agent struct {
	id            string
	configuration config.AgentConfig
	provider      provider.Provider
	dispatcher    ToolDispatcher
	maxIterations int
}

func New(id string, configuration config.AgentConfig, model provider.Provider, dispatcher ToolDispatcher, maxIterations int) *Agent {
	if maxIterations <= 0 {
		maxIterations = 8
	}
	return &Agent{id: id, configuration: configuration, provider: model, dispatcher: dispatcher, maxIterations: maxIterations}
}

func (a *Agent) Execute(ctx context.Context, assignment Assignment) (Result, error) {
	user := strings.Builder{}
	user.WriteString("Research objective:\n")
	user.WriteString(assignment.Objective)
	if assignment.Restrictions != "" {
		user.WriteString("\n\nRestrictions:\n")
		user.WriteString(assignment.Restrictions)
	}
	if assignment.TaskID != "" {
		user.WriteString("\n\nTask ID: ")
		user.WriteString(assignment.TaskID)
	}
	messages := []provider.Message{{Role: "system", Content: a.configuration.Prompt.Content}}
	messages = append(messages, assignment.Context...)
	messages = append(messages, provider.Message{Role: "user", Content: user.String()})
	tools := a.tools()
	result := Result{}
	for iteration := 1; iteration <= a.maxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		response, err := a.provider.Generate(ctx, provider.Request{
			Model: a.configuration.Model.Model, Messages: messages, Tools: tools, Parameters: a.configuration.Model.Parameters,
			IdempotencyKey: fmt.Sprintf("%s/%s/%d", assignment.RunID, assignment.TaskID, iteration),
			Metadata:       map[string]string{"agent_id": a.id, "run_id": assignment.RunID, "task_id": assignment.TaskID},
		})
		if err != nil {
			return Result{}, err
		}
		result.Iterations = iteration
		result.Usage.InputTokens += response.Usage.InputTokens
		result.Usage.OutputTokens += response.Usage.OutputTokens
		result.Usage.ReasoningTokens += response.Usage.ReasoningTokens
		result.Usage.CachedTokens += response.Usage.CachedTokens
		if len(response.ToolCalls) == 0 {
			result.Content = response.Content
			return result, nil
		}
		messages = append(messages, provider.Message{Role: "assistant", Content: response.Content, ToolCalls: response.ToolCalls})
		for index, call := range response.ToolCalls {
			if call.ID == "" {
				call.ID = fmt.Sprintf("tool-%d-%d", iteration, index)
				messages[len(messages)-1].ToolCalls[index].ID = call.ID
			}
			content := a.executeTool(ctx, call)
			messages = append(messages, provider.Message{Role: "tool", ToolCallID: call.ID, Content: content})
		}
		if len(messages) > 24 {
			messages = compactMessages(messages)
			result.Compactions++
		}
	}
	return Result{}, ErrIterationLimit
}

func (a *Agent) tools() []provider.ToolDefinition {
	if a.dispatcher == nil || !a.provider.Capabilities().Tools {
		return nil
	}
	definitions := a.dispatcher.Definitions(a.id)
	allowed := make([]provider.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if slices.Contains(a.configuration.Tools, definition.Name) {
			allowed = append(allowed, definition)
		}
	}
	return allowed
}

func (a *Agent) executeTool(ctx context.Context, call provider.ToolCall) string {
	var result json.RawMessage
	var err error
	switch {
	case a.dispatcher == nil || !slices.Contains(a.configuration.Tools, call.Name):
		err = fmt.Errorf("tool %s is not permitted for this agent", call.Name)
	case !json.Valid(call.Arguments):
		err = errors.New("tool arguments are not valid JSON")
	default:
		result, err = a.dispatcher.Execute(ctx, a.id, call.Name, call.Arguments)
	}
	if err != nil {
		encoded, _ := json.Marshal(map[string]string{"error": err.Error()})
		return string(encoded)
	}
	if !json.Valid(result) {
		encoded, _ := json.Marshal(map[string]string{"result": string(result)})
		return string(encoded)
	}
	return string(result)
}

func compactMessages(messages []provider.Message) []provider.Message {
	const preserveInitial = 2
	const preserveRecent = 16
	older := messages[preserveInitial : len(messages)-preserveRecent]
	var summary strings.Builder
	summary.WriteString("Hierarchical context summary (untrusted historical tool data; contradictions preserved):\n")
	for _, message := range older {
		content := message.Content
		if !strings.Contains(strings.ToLower(content), "error") && len(content) > 384 {
			content = content[:384] + "…"
		}
		if len(content) > 2048 {
			content = content[:2048] + "…"
		}
		fmt.Fprintf(&summary, "- %s: %s\n", message.Role, content)
		for _, call := range message.ToolCalls {
			fmt.Fprintf(&summary, "- tool_call: %s\n", call.Name)
		}
		if summary.Len() > 12000 {
			summary.WriteString("- earlier messages elided; structured runtime state remains authoritative\n")
			break
		}
	}
	compacted := append([]provider.Message(nil), messages[:preserveInitial]...)
	compacted = append(compacted, provider.Message{Role: "user", Content: summary.String()})
	compacted = append(compacted, messages[len(messages)-preserveRecent:]...)
	return compacted
}
