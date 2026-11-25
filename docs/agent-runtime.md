# Agent Runtime

## Composition

An agent combines identity, role, model provider or external backend, system prompt, tool registry, durable task and mailbox references, memory policy, permissions, lifecycle, and budget allocation. Native agents use a provider tool-call loop. External backends are adapters behind `AgentBackend`; they do not receive direct storage access.

## Provider and backend interfaces

```go
type Provider interface {
    Generate(context.Context, Request) (Response, error)
    Stream(context.Context, Request) (<-chan Event, error)
    Capabilities() Capabilities
}

type AgentBackend interface {
    Start(context.Context, Task) error
    Send(context.Context, Message) error
    Events() <-chan Event
    Stop() error
}
```

External adapters must use an official structured protocol such as ACP or a supported SDK. Terminal scraping is not an integration strategy.

## Native loop

The native runner builds bounded context from durable state, invokes the provider, validates returned tool calls, appends tool results, and stops on final output or cancellation. Every iteration has a maximum count, deadline, and budget reservation. Tool output and retrieved content are marked untrusted.

## Communication

Every message has a stable ID, run ID, sender, recipient, optional task ID, timestamp, type, payload, correlation ID, and idempotency key. Direct messages, delegation, results, clarification, broadcasts, findings, criticism, experiment requests, and human directives share one durable envelope with schema-specific payloads.

Delivery is at least once. Consumers deduplicate by message ID or operation idempotency key. A mailbox survives restarts, and human directives can be queued while an agent waits for a provider or executor.

## Context and memory

Context is assembled from the current task, recent relevant messages, structured findings, summaries, and artifact references. Bounded hierarchical compaction preserves tool errors and contradictory evidence while retaining recent messages and provenance. Original artifacts remain inspectable. No scientific state exists only in a conversation transcript.

## Health

The runtime records process heartbeat, active generation, provider wait, executor wait, last tool result, and last meaningful output separately. Cancellation propagates through contexts. A provider retry is not considered scientific progress.

## External agents

Kimi CLI, Codex, OpenCode, and ACP adapters are extension points, not claimed integrations. Each requires an official noninteractive protocol, explicit tool and filesystem policy, cancellation tests, and resource accounting before documentation marks it supported.
