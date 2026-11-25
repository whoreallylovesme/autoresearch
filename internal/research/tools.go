package research

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/whoreallylovesme/autoresearch/internal/messaging"
	"github.com/whoreallylovesme/autoresearch/internal/provider"
	"github.com/whoreallylovesme/autoresearch/internal/scheduler"
)

type taskArguments struct {
	TaskID         string   `json:"task_id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Role           string   `json:"role"`
	Priority       int      `json:"priority"`
	Dependencies   []string `json:"dependencies"`
	IdempotencyKey string   `json:"idempotency_key"`
}

type findingArguments struct {
	TaskID   string   `json:"task_id"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence"`
}

func (r *Runtime) Definitions(agentID string) []provider.ToolDefinition {
	r.mu.Lock()
	current := r.agents[agentID]
	tools := append([]string(nil), current.configuration.Tools...)
	r.mu.Unlock()
	sort.Strings(tools)
	definitions := make([]provider.ToolDefinition, 0, len(tools))
	for _, name := range tools {
		definitions = append(definitions, provider.ToolDefinition{Name: name, Description: toolDescription(name), Parameters: toolSchema(name)})
	}
	return definitions
}

func (r *Runtime) Execute(ctx context.Context, agentID, name string, arguments json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "create_task", "delegate_task":
		var input taskArguments
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		return r.createTask(agentID, input)
	case "query_knowledge":
		var input struct {
			Query        string `json:"query"`
			WaitForTasks bool   `json:"wait_for_tasks"`
		}
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		if input.WaitForTasks {
			if err := r.waitForTasks(ctx); err != nil {
				return nil, err
			}
		}
		return json.Marshal(map[string]any{"query": input.Query, "findings": r.Findings()})
	case "submit_finding", "submit_criticism":
		var input findingArguments
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		if strings.TrimSpace(input.Summary) == "" || input.TaskID == "" {
			return nil, errors.New("task_id and summary are required")
		}
		r.mu.Lock()
		current := r.agents[agentID]
		kind := "finding"
		if name == "submit_criticism" {
			kind = "criticism"
		}
		finding := r.addFindingLocked(agentID, current.configuration.Role, input.TaskID, kind, input.Summary, input.Evidence)
		r.persistFindingLocked(finding)
		r.mu.Unlock()
		return json.RawMessage(`{"accepted":true}`), nil
	case "get_budget_status":
		return json.Marshal(r.budget.Status())
	case "finalize_research":
		return r.finalize(agentID)
	case "list_agents":
		return json.Marshal(r.Agents())
	case "get_agent_status":
		var input struct {
			AgentID string `json:"agent_id"`
		}
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		for _, status := range r.Agents() {
			if status.ID == input.AgentID {
				return json.Marshal(status)
			}
		}
		return nil, errors.New("agent not found")
	case "cancel_task":
		var input struct {
			TaskID string `json:"task_id"`
		}
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		if err := r.scheduler.Cancel(input.TaskID); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"cancelled":true}`), nil
	case "send_message":
		var input struct {
			Recipient string          `json:"recipient"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := decode(arguments, &input); err != nil {
			return nil, err
		}
		message, err := messaging.New(r.status.ID, agentID, input.Recipient, messaging.TypeDirect, input.Payload)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.messages = append(r.messages, message)
		r.persistMessageLocked(message)
		r.mu.Unlock()
		return json.Marshal(map[string]string{"message_id": message.ID})
	default:
		return nil, fmt.Errorf("tool %s is not available in the minimal runtime", name)
	}
}

func (r *Runtime) createTask(agentID string, input taskArguments) (json.RawMessage, error) {
	if strings.TrimSpace(input.Title) == "" || strings.TrimSpace(input.Description) == "" || input.IdempotencyKey == "" {
		return nil, errors.New("title, description, and idempotency_key are required")
	}
	if input.TaskID != "" && !regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`).MatchString(input.TaskID) {
		return nil, errors.New("invalid task_id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.State != StateRunning {
		return nil, errors.New("research is not accepting new tasks")
	}
	roleAllowed := false
	for _, current := range r.agents {
		if current.configuration.Role == input.Role && input.Role != "chief-scientist" {
			roleAllowed = true
		}
	}
	if !roleAllowed {
		return nil, fmt.Errorf("no configured worker role %q", input.Role)
	}
	r.nextTask++
	if input.TaskID == "" {
		input.TaskID = fmt.Sprintf("task-%d", r.nextTask)
	}
	task := scheduler.Task{ID: input.TaskID, RunID: r.status.ID, Title: input.Title, Description: input.Description, Role: input.Role, Priority: input.Priority, Dependencies: input.Dependencies, IdempotencyKey: agentID + ":" + input.IdempotencyKey, MaxAttempts: 2}
	if err := r.scheduler.Submit(task); err != nil {
		return nil, err
	}
	r.persistTaskEventLocked(input.TaskID, "task.created", agentID, task)
	return json.Marshal(map[string]string{"task_id": input.TaskID})
}

func (r *Runtime) waitForTasks(ctx context.Context) error {
	for {
		tasks := r.scheduler.List()
		terminal := len(tasks) > 0
		for _, task := range tasks {
			switch task.State {
			case scheduler.StateCompleted:
			case scheduler.StateFailed, scheduler.StateCancelled:
				return fmt.Errorf("task %s did not complete: %s", task.ID, task.LastError)
			default:
				terminal = false
			}
		}
		if terminal {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (r *Runtime) finalize(agentID string) (json.RawMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := r.scheduler.List()
	if len(tasks) == 0 {
		return nil, errors.New("cannot finalize without delegated tasks")
	}
	for _, task := range tasks {
		if task.State != scheduler.StateCompleted {
			return nil, fmt.Errorf("task %s is %s, not completed", task.ID, task.State)
		}
	}
	criticism := false
	for _, finding := range r.findings {
		if finding.Kind == "criticism" {
			criticism = true
		}
	}
	if !criticism {
		return nil, errors.New("cannot finalize without independent criticism")
	}
	r.status.State = StateFinalizing
	r.budget.Finalize()
	r.recordEventLocked("research.finalizing", agentID, "", nil)
	var report strings.Builder
	report.WriteString("# Research Report\n\n## Objective\n\n")
	report.WriteString(r.configuration.Task.Objective)
	report.WriteString("\n\n## Task results\n\nThese are recorded agent results; verification scope and limitations remain explicit.\n\n")
	for _, finding := range r.findings {
		fmt.Fprintf(&report, "- **%s** (`%s`, %s): %s\n", finding.TaskID, finding.AgentID, finding.Kind, finding.Summary)
	}
	report.WriteString("\n## Limitations\n\nNo claim is objectively verified merely because an agent produced it.\n")
	r.report = report.String()
	r.persistArtifactLocked("research-report.md", "text/markdown", []byte(r.report))
	r.status.State = StateCompleted
	r.status.EndedAt = time.Now().UTC()
	r.recordEventLocked("research.completed", agentID, "", map[string]int{"findings": len(r.findings)})
	return json.Marshal(map[string]string{"status": "completed", "report": r.report})
}

func decode(arguments json.RawMessage, destination any) error {
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	if decoder.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func toolDescription(name string) string {
	descriptions := map[string]string{
		"create_task": "Create a validated research task", "delegate_task": "Delegate a task to a configured worker role",
		"query_knowledge": "Query structured findings and optionally wait for delegated tasks", "submit_finding": "Submit an evidence-linked finding",
		"submit_criticism": "Submit independent scientific criticism", "get_budget_status": "Read budget state",
		"finalize_research": "Finalize after all tasks and independent criticism are complete", "list_agents": "List agent states",
		"get_agent_status": "Inspect one agent", "cancel_task": "Cancel a task", "send_message": "Send a structured message",
	}
	if description := descriptions[name]; description != "" {
		return description
	}
	return "Runtime-controlled scientific tool"
}

func toolSchema(name string) json.RawMessage {
	schemas := map[string]string{
		"create_task":      `{"type":"object","required":["title","description","role","idempotency_key"],"properties":{"task_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"role":{"type":"string"},"priority":{"type":"integer"},"dependencies":{"type":"array","items":{"type":"string"}},"idempotency_key":{"type":"string"}},"additionalProperties":false}`,
		"delegate_task":    `{"type":"object","required":["title","description","role","idempotency_key"],"properties":{"task_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"role":{"type":"string"},"priority":{"type":"integer"},"dependencies":{"type":"array","items":{"type":"string"}},"idempotency_key":{"type":"string"}},"additionalProperties":false}`,
		"query_knowledge":  `{"type":"object","properties":{"query":{"type":"string"},"wait_for_tasks":{"type":"boolean"}},"additionalProperties":false}`,
		"submit_finding":   `{"type":"object","required":["task_id","summary"],"properties":{"task_id":{"type":"string"},"summary":{"type":"string"},"evidence":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`,
		"submit_criticism": `{"type":"object","required":["task_id","summary"],"properties":{"task_id":{"type":"string"},"summary":{"type":"string"},"evidence":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`,
		"get_agent_status": `{"type":"object","required":["agent_id"],"properties":{"agent_id":{"type":"string"}},"additionalProperties":false}`,
		"cancel_task":      `{"type":"object","required":["task_id"],"properties":{"task_id":{"type":"string"}},"additionalProperties":false}`,
		"send_message":     `{"type":"object","required":["recipient","payload"],"properties":{"recipient":{"type":"string"},"payload":{"type":"object"}},"additionalProperties":false}`,
	}
	if schema := schemas[name]; schema != "" {
		return json.RawMessage(schema)
	}
	return json.RawMessage(`{"type":"object","additionalProperties":false}`)
}
