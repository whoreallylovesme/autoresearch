package provider

import (
	"context"
	"errors"
	"sync"
)

type HandlerFunc func(context.Context, Request) (Response, error)

type MockProvider struct {
	mu        sync.Mutex
	responses []Response
	requests  []Request
	handler   HandlerFunc
}

func NewMock(responses ...Response) *MockProvider {
	return &MockProvider{responses: append([]Response(nil), responses...)}
}

func NewMockHandler(handler HandlerFunc) *MockProvider {
	return &MockProvider{handler: handler}
}

func (m *MockProvider) Generate(ctx context.Context, request Request) (Response, error) {
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	m.mu.Lock()
	m.requests = append(m.requests, request)
	handler := m.handler
	if handler == nil {
		if len(m.responses) == 0 {
			m.mu.Unlock()
			return Response{}, errors.New("mock provider has no scripted response")
		}
		response := m.responses[0]
		m.responses = m.responses[1:]
		m.mu.Unlock()
		return response, nil
	}
	m.mu.Unlock()
	return handler(ctx, request)
}

func (m *MockProvider) Stream(ctx context.Context, request Request) (<-chan Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	events := make(chan Event, 3)
	go func() {
		defer close(events)
		response, err := m.Generate(ctx, request)
		if err != nil {
			events <- Event{Type: EventError, Err: err}
			return
		}
		select {
		case events <- Event{Type: EventContent, Delta: response.Content}:
		case <-ctx.Done():
			events <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		events <- Event{Type: EventDone, Response: response}
	}()
	return events, nil
}

func (m *MockProvider) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Tools: true, StructuredOutputs: true, SupportedParameters: []string{"temperature", "max_output_tokens"}}
}

func (m *MockProvider) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.requests...)
}
