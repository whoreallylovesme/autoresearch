# Provider Integration

## Supported abstraction

Providers implement text generation, streaming, tools when supported, structured responses, usage accounting, cancellation, error classification, rate limiting, retries, and capability reporting. Agent backends are separate: a raw completion API is not automatically an agent harness. Capabilities that an adapter does not expose are reported as unavailable rather than emulated silently.

## Hosted OpenAI and Moonshot

The OpenAI-compatible Chat Completions client supports non-streaming generation, tool calls, streaming content/usage, cancellation, shared endpoint rate limits, and bounded retries through local HTTP tests. `KimiProvider` is a dedicated hosted-Moonshot profile over that transport. It defaults to `https://api.moonshot.ai/v1`, uses the configured API-key environment variable, maps portable token limits to `max_tokens`, supports declared reasoning levels and JSON mode, and reads reported usage fields. Capabilities can be restricted explicitly in YAML.

`configs/mixed-providers.example.yaml` demonstrates an OpenAI/Astra Chief with independent hosted-Kimi workers. The mixed-fleet integration test uses separate fake endpoints and credentials; it does not spend provider credits.

Official hosted API references:

- [OpenAI Chat Completions](https://platform.openai.com/docs/api-reference/chat/create)
- [Kimi Chat Completions](https://platform.moonshot.ai/docs/api/chat)
- [Kimi OpenAI compatibility](https://platform.moonshot.ai/docs/guide/migrating-from-openai-to-kimi)

## Native Kimi Code: `kimi-web`

`provider: kimi-web` is a separate native adapter for the official [`kimi web`](https://moonshotai.github.io/kimi-code/en/reference/kimi-command.html) server. It uses the personal Kimi Code authentication configured by the operator through `kimi login`, not `MOONSHOT_API_KEY`. The local server exposes the experimental Kimi Agent Protocol (KAP) over REST and WebSocket. This implementation was checked against Kimi Code 2.8.0 and its live `/api/v1/kap.md` and `/openapi.json`; use those version-specific schemas instead of assuming that a newer server is compatible.

Each configured worker receives its own `KimiWebProvider`, native `session_id`, and `session_workspace/agent_id`. The shared server is only a transport. Session creation uses `workspace_root` and `permission_mode: manual`; workspace paths and agent identifiers are validated. No conversation history is shared between agents.

The provider follows KAP's turn lifecycle:

1. Create a native session lazily.
2. Submit a unique idempotent `prompt_id` with only messages not already present in that native session. If runtime compaction invalidates the prefix, send an explicit authoritative state replacement.
3. Wait with `/api/v1/sessions/{id}:wait`; do not infer completion from the last message.
4. Read `/agents/main/turns`, correlate the user turn to the prompt ID, and extract only assistant messages from that turn and its continuations.
5. Cancel with `/:cancel`, including background work, on shutdown or context cancellation.

Runtime tools are described in the native prompt and returned through a strict JSON envelope containing either `tool_calls` or `final_response`. Tool outputs are marked untrusted, and malformed or uncorrelated responses are errors. This is the runtime tool bridge, not native Kimi function calling. Streaming and generic structured-output capability flags remain disabled.

Kimi built-in approvals and questions are never auto-approved. Only `permission_mode: manual` is accepted because all sessions in the current native service share an OS identity; `auto`/`yolo` require a future per-agent execution boundary. Runtime-authorized tools remain autonomous inside their existing Go policies.

Subscription-backed native calls require request quotas and rate limits, not per-token pricing. Monetary expenditure and token counts are not invented when KAP does not report them. Existing provider instances must be recreated after server-token rotation. Restart recovery creates fresh native sessions and relies on durable runtime research state; old native sessions remain in Kimi's private history.

`make native-kimi-test` verifies a real native server, distinct sessions/workspaces, and the private token handoff without OAuth or prompts. Fake-KAP tests cover multi-turn tool calls, response correlation, cancellation, retries, and the OpenAI-Chief/native-worker fleet. Live OAuth and paid/subscription generation are not part of development tests and have not been performed.

## Capability validation and failures

Adapters accept only declared parameters and configured capabilities. Unsupported token limits, reasoning levels, permission modes, or provider extensions are errors rather than silently ignored fields. Model names, quotas, reasoning levels, and hosted prices are configuration, not hardcoded availability claims. Startup validation is local; live model discovery and account access require explicit operator action.

Retry is limited to classified transient transport, throttling, and server errors with bounded exponential backoff, deadlines, and cancellation. Authentication, permission, invalid request, malformed protocol data, and unsupported capabilities are permanent. Prompt IDs make native retries idempotent; a lost session-creation response can leave an empty unused session, so session creation is not claimed to be exactly once.

## Deterministic and local test models

`MockProvider` is deterministic, scripted, credential-free, and zero-cost. A separate OpenAI-compatible local provider can target the pinned Qwen2.5 0.5B GGUF model through llama.cpp. `HFMockProvider` supplies deterministic tool plans and bounded local narration; fabricated/source-like narration is rejected. The small model is not a scientific oracle and is not required by unit tests.

- [Qwen2.5 0.5B Instruct GGUF model card](https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF)

No development test may spend paid API credits or silently consume subscription-backed generation. Unattended workloads require authentication and terms that explicitly permit automation.
