# Testing

All tests run without paid APIs or GPUs. Deterministic mock providers and executors control timing, failures, and outputs. Concurrency-sensitive Go packages use the race detector.

## Commands

```bash
make fmt-check
make lint
make test
make test-integration
make validate
make run-demo
make literature-demo
make experiment-demo
make full-research-demo
make local-model-demo
make web-lint
make web-test
make browser-test
```

The canonical CI workflow runs `go vet`, `go test -race -shuffle=on ./...`, integration tests, Python Ruff checks, and pytest when those components exist.

## Required coverage

- Strict configuration, unknown fields, cycles, references, capabilities, and snapshots.
- Provider cancellation, retries, throttling, malformed responses, and usage.
- Scheduler dependencies, priorities, leases, retries, duplicate suppression, and backpressure.
- Concurrent budget reservations, reconciliation, exhaustion, and finalization reserve.
- Message persistence, event ordering, idempotency, and reconnect.
- Forced-process restart at transactional crash points.
- Graph transition and verification invariants.
- Paper cache deduplication, source attribution, and untrusted content.
- Sandbox filesystem/network/secrets restrictions, timeouts, and resource limits.
- Artifact checksums and immutable result history.
- API contracts and Python control-center behavior.
- Headless Chromium acceptance for live events, graph, Plotly, experiments, fleet links, and persistent chat.

## Integration recovery test

Two process-level tests exercise recovery. The runtime helper commits one result, blocks on a second task, receives `SIGKILL`, and resumes in a new process. The daemon acceptance test starts the actual `researchd` with `AUTORESEARCH_DEMO_DELAY=2s` for deterministic fault timing, kills it with `SIGKILL`, restarts it on the same private data directory, and verifies three unique completed tasks/findings/messages, an artifact, ordered events, and no open budget reservations.

## Demonstration

The deterministic demos exercise delegation, criticism, budget accounting, durable events, literature, experiments, and final reports. `full-research-demo` combines literature, hypotheses, sandboxed execution, verification, and playback in one session. `local-model-demo` downloads and SHA-verifies Qwen GGUF, runs llama.cpp, and proves a real autonomous run with bounded HF narration and chat.
