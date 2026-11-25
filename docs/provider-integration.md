# Provider Integration

## Supported abstraction

Providers implement text generation, streaming, tools when supported, structured responses, usage accounting, cancellation, error classification, rate limiting, retries, and capability reporting. Agent backends are separate: a raw completion API is not an agent harness.

The implemented real transport is an OpenAI-compatible Chat Completions client with explicit provider profiles for OpenAI, Kimi/Moonshot, and local endpoints. Non-streaming generation, tool calls, streaming content/usage, cancellation, rate limits, and bounded retries are covered by local HTTP tests. No paid provider call has been made. Other providers and external agent harnesses require independent adapters and conformance tests.

## Capability validation

The adapter accepts only parameters declared by its profile and configured capabilities. It never silently maps `reasoning_effort`, temperature, token limits, or provider extensions to unsupported fields. Model names, prices, quotas, and reasoning levels are configuration, not constants.

Official references checked during initial design:

- [OpenAI Chat Completions API](https://platform.openai.com/docs/api-reference/chat/create): messages, function tools, streaming chunks, and usage semantics. `max_output_tokens` is an internal portable field and must map to the currently supported provider request field.
- [Kimi Chat Completions API](https://platform.moonshot.ai/docs/api/chat) and [OpenAI compatibility guide](https://platform.moonshot.ai/docs/guide/migrating-from-openai-to-kimi): OpenAI-compatible transport, tools, streaming, and Kimi-specific extensions.
- [Qwen2.5 0.5B Instruct GGUF model card](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF): 0.49B parameters, quantized GGUF artifacts, llama.cpp usage, and context limitations.

Documentation is not a guarantee of account access. Startup performs local contract validation; optional live capability checks must be explicit and budgeted.

## Errors and retry

Retry only classified transient transport, throttling, and server errors, with bounded exponential backoff, jitter, attempt limits, deadline, and context cancellation. Authentication, permission, invalid request, and unsupported capability failures are permanent. Usage is reconciled even when a response arrives after a timeout where possible.

## Deterministic and local test models

`MockProvider` is implemented as a deterministic scripted provider for unit and integration tests; it performs no network calls and has zero monetary cost. Real provider runs additionally require explicit token pricing and conservative maximum-input estimation in the model YAML. A separate OpenAI-compatible local provider can target a small Hugging Face model served by llama.cpp for realistic credential-free inference. The local model is not used as a deterministic oracle and is never required by unit tests.

No development test may spend paid API credits. Unattended use requires provider authentication and terms that explicitly permit automation.
