package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type Request struct {
	Model          string            `json:"model,omitempty"`
	Messages       []Message         `json:"messages"`
	Tools          []ToolDefinition  `json:"tools,omitempty"`
	Parameters     map[string]any    `json:"parameters,omitempty"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Response struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
	Usage        Usage      `json:"usage"`
}

type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	CachedTokens     int64 `json:"cached_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
}

type Capabilities struct {
	Streaming           bool
	Tools               bool
	StructuredOutputs   bool
	SupportedParameters []string
	ReasoningEfforts    []string
}

type EventType string

const (
	EventContent EventType = "content"
	EventDone    EventType = "done"
	EventError   EventType = "error"
)

type Event struct {
	Type     EventType
	Delta    string
	Response Response
	Err      error
}

type Provider interface {
	Generate(context.Context, Request) (Response, error)
	Stream(context.Context, Request) (<-chan Event, error)
	Capabilities() Capabilities
}

type ErrorKind string

const (
	ErrorTransient      ErrorKind = "transient"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorAuthentication ErrorKind = "authentication"
	ErrorPermanent      ErrorKind = "permanent"
)

type Error struct {
	Kind       ErrorKind
	StatusCode int
	Err        error
}

func (e *Error) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("provider %s error (HTTP %d)", e.Kind, e.StatusCode)
	}
	if e.Err != nil {
		return fmt.Sprintf("provider %s error: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("provider %s error", e.Kind)
}

func (e *Error) Unwrap() error { return e.Err }
