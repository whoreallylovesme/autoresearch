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

## Integration recovery test

A subprocess starts a real run with a controllable provider, commits one result, blocks on a second task, and is terminated with `SIGKILL`. A new process opens the same database, fences the old lease, resumes pending work, and verifies no completed result or accepted message is lost or duplicated.

## Demonstration

The deterministic demo exercises supervisor delegation, multiple workers, independent criticism, budget accounting, durable events, and a final evidence-linked report. A separate local Hugging Face model smoke test checks the OpenAI-compatible provider; it is not a deterministic test oracle.
