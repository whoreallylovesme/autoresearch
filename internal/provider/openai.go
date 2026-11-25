package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type RateLimiter interface {
	wait(context.Context) error
}

func NewRateLimiter(requestsPerMinute int) RateLimiter {
	if requestsPerMinute <= 0 {
		return nil
	}
	return &requestLimiter{interval: time.Minute / time.Duration(requestsPerMinute)}
}

type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

type OpenAIOptions struct {
	Provider          string
	Model             string
	BaseURL           string
	APIKey            string
	HTTPClient        *http.Client
	Capabilities      Capabilities
	Retry             RetryPolicy
	RequestsPerMinute int
	Limiter           RateLimiter
}

type OpenAIProvider struct {
	provider     string
	model        string
	endpoint     string
	apiKey       string
	client       *http.Client
	capabilities Capabilities
	retry        RetryPolicy
	limiter      RateLimiter
}

type requestLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func NewOpenAI(options OpenAIOptions) (*OpenAIProvider, error) {
	if options.Model == "" {
		return nil, errors.New("model is required")
	}
	parsed, err := url.Parse(strings.TrimRight(options.BaseURL, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("a valid base URL is required")
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 15 * time.Minute}
	}
	if options.Retry.MaxAttempts == 0 {
		options.Retry.MaxAttempts = 3
	}
	if options.Retry.MaxAttempts < 1 || options.Retry.InitialBackoff < 0 || options.Retry.MaxBackoff < options.Retry.InitialBackoff {
		return nil, errors.New("invalid retry policy")
	}
	if options.RequestsPerMinute < 0 {
		return nil, errors.New("invalid rate limit")
	}
	provider := &OpenAIProvider{
		provider: options.Provider, model: options.Model, endpoint: parsed.String() + "/chat/completions",
		apiKey: options.APIKey, client: options.HTTPClient, capabilities: options.Capabilities, retry: options.Retry,
	}
	provider.limiter = options.Limiter
	if provider.limiter == nil && options.RequestsPerMinute > 0 {
		provider.limiter = NewRateLimiter(options.RequestsPerMinute)
	}
	return provider, nil
}

func (p *OpenAIProvider) Capabilities() Capabilities {
	capabilities := p.capabilities
	capabilities.SupportedParameters = append([]string(nil), capabilities.SupportedParameters...)
	return capabilities
}

func (p *OpenAIProvider) Generate(ctx context.Context, request Request) (Response, error) {
	body, err := p.requestBody(request, false)
	if err != nil {
		return Response{}, err
	}
	for attempt := 0; attempt < p.retry.MaxAttempts; attempt++ {
		if p.limiter != nil {
			if err := p.limiter.wait(ctx); err != nil {
				return Response{}, err
			}
		}
		response, err := p.roundTrip(ctx, body)
		if err == nil {
			return response, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Response{}, ctxErr
		}
		if !retryable(err) || attempt == p.retry.MaxAttempts-1 {
			return Response{}, err
		}
		backoff := p.retry.InitialBackoff << attempt
		if backoff > p.retry.MaxBackoff {
			backoff = p.retry.MaxBackoff
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func (p *OpenAIProvider) roundTrip(ctx context.Context, body []byte) (Response, error) {
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, &Error{Kind: ErrorPermanent, Err: err}
	}
	p.setHeaders(httpRequest)
	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, &Error{Kind: ErrorTransient, Err: err}
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, 1<<20))
		return Response{}, classifyStatus(httpResponse.StatusCode)
	}
	var wire wireResponse
	decoder := json.NewDecoder(io.LimitReader(httpResponse.Body, 16<<20))
	if err := decoder.Decode(&wire); err != nil {
		return Response{}, &Error{Kind: ErrorPermanent, Err: fmt.Errorf("decode response: %w", err)}
	}
	if len(wire.Choices) == 0 {
		return Response{}, &Error{Kind: ErrorPermanent, Err: errors.New("response contains no choices")}
	}
	choice := wire.Choices[0]
	return responseFromWire(wire.ID, wire.Model, choice.Message, choice.FinishReason, wire.Usage), nil
}

func (p *OpenAIProvider) Stream(ctx context.Context, request Request) (<-chan Event, error) {
	if !p.capabilities.Streaming {
		return nil, &Error{Kind: ErrorPermanent, Err: errors.New("streaming is not supported")}
	}
	body, err := p.requestBody(request, true)
	if err != nil {
		return nil, err
	}
	if p.limiter != nil {
		if err := p.limiter.wait(ctx); err != nil {
			return nil, err
		}
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p.setHeaders(httpRequest)
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return nil, &Error{Kind: ErrorTransient, Err: err}
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		defer httpResponse.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(httpResponse.Body, 1<<20))
		return nil, classifyStatus(httpResponse.StatusCode)
	}
	events := make(chan Event, 16)
	go p.consumeStream(ctx, httpResponse, events)
	return events, nil
}

func (p *OpenAIProvider) consumeStream(ctx context.Context, response *http.Response, events chan<- Event) {
	defer close(events)
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	final := Response{}
	done := false
	send := func(event Event) bool {
		select {
		case events <- event:
			return true
		case <-ctx.Done():
			return false
		}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk wireResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			send(Event{Type: EventError, Err: &Error{Kind: ErrorPermanent, Err: err}})
			return
		}
		final.ID = chunk.ID
		final.Model = chunk.Model
		final.Usage = usageFromWire(chunk.Usage)
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				final.Content += choice.Delta.Content
				if !send(Event{Type: EventContent, Delta: choice.Delta.Content}) {
					return
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				for len(final.ToolCalls) <= call.Index {
					final.ToolCalls = append(final.ToolCalls, ToolCall{})
				}
				current := &final.ToolCalls[call.Index]
				if call.ID != "" {
					current.ID = call.ID
				}
				if call.Function.Name != "" {
					current.Name = call.Function.Name
				}
				current.Arguments = append(current.Arguments, call.Function.Arguments...)
			}
			if choice.FinishReason != "" {
				final.FinishReason = choice.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil {
		send(Event{Type: EventError, Err: &Error{Kind: ErrorTransient, Err: err}})
		return
	}
	if !done {
		send(Event{Type: EventError, Err: &Error{Kind: ErrorTransient, Err: io.ErrUnexpectedEOF}})
		return
	}
	send(Event{Type: EventDone, Response: final})
}

func (p *OpenAIProvider) requestBody(request Request, stream bool) ([]byte, error) {
	model := request.Model
	if model == "" {
		model = p.model
	}
	messages := make([]wireMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		wire := wireMessage{Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID}
		for _, call := range message.ToolCalls {
			wire.ToolCalls = append(wire.ToolCalls, wireToolCall{ID: call.ID, Type: "function", Function: wireFunction{Name: call.Name, Arguments: string(call.Arguments)}})
		}
		messages = append(messages, wire)
	}
	body := map[string]any{"model": model, "messages": messages}
	if stream {
		body["stream"] = true
		if p.provider == "openai" || p.provider == "kimi" {
			body["stream_options"] = map[string]any{"include_usage": true}
		}
	}
	for name, value := range request.Parameters {
		if !slices.Contains(p.capabilities.SupportedParameters, name) {
			return nil, &Error{Kind: ErrorPermanent, Err: fmt.Errorf("unsupported model parameter %q", name)}
		}
		switch name {
		case "max_output_tokens":
			if p.provider == "openai" {
				body["max_completion_tokens"] = value
			} else {
				body["max_tokens"] = value
			}
		default:
			body[name] = value
		}
	}
	if len(request.Tools) > 0 {
		if !p.capabilities.Tools {
			return nil, &Error{Kind: ErrorPermanent, Err: errors.New("tool calling is not supported")}
		}
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			if tool.Name == "" || !json.Valid(tool.Parameters) {
				return nil, &Error{Kind: ErrorPermanent, Err: errors.New("invalid tool definition")}
			}
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.Parameters)}})
		}
		body["tools"] = tools
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, &Error{Kind: ErrorPermanent, Err: err}
	}
	return encoded, nil
}

func (p *OpenAIProvider) setHeaders(request *http.Request) {
	request.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
}

func classifyStatus(status int) error {
	kind := ErrorPermanent
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		kind = ErrorAuthentication
	} else if status == http.StatusTooManyRequests {
		kind = ErrorRateLimit
	} else if status == http.StatusRequestTimeout || status >= 500 {
		kind = ErrorTransient
	}
	return &Error{Kind: kind, StatusCode: status}
}

func retryable(err error) bool {
	var providerError *Error
	return errors.As(err, &providerError) && (providerError.Kind == ErrorTransient || providerError.Kind == ErrorRateLimit)
}

func (l *requestLimiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	reserved := l.next
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()
	wait := reserved.Sub(now)
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
}

type wireToolCall struct {
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function wireFunction `json:"function"`
}

type wireFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireChoice struct {
	Message      wireMessage `json:"message"`
	Delta        wireMessage `json:"delta"`
	FinishReason string      `json:"finish_reason"`
}

type wireUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type wireResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   wireUsage    `json:"usage"`
}

func responseFromWire(id, model string, message wireMessage, finish string, usage wireUsage) Response {
	response := Response{ID: id, Model: model, Content: message.Content, FinishReason: finish, Usage: usageFromWire(usage)}
	for _, call := range message.ToolCalls {
		response.ToolCalls = append(response.ToolCalls, ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments)})
	}
	return response
}

func usageFromWire(usage wireUsage) Usage {
	return Usage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens, CachedTokens: usage.PromptTokensDetails.CachedTokens, ReasoningTokens: usage.CompletionTokensDetails.ReasoningTokens}
}
