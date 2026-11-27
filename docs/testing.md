# Testing

All tests run without paid APIs or GPUs. Deterministic mock providers and executors control timing, failures, and outputs. Concurrency-sensitive Go packages use the race detector. No external model-provider credentials are needed for the deterministic suites. Local operator authentication is mandatory: test harnesses use test-only values or generated ephemeral token files, while demo wrappers reuse an explicitly configured valid token file and otherwise generate and clean one up automatically.

Go package tests live beside their source in `*_test.go` files; they are excluded from production binaries. Use `package foo_test` to exercise the public contract and `package foo` only for intentional internal white-box coverage. The top-level `tests/` directory is reserved for cross-package, subprocess, daemon-recovery, local-model, and real-service integration tests.

## Commands

```bash
make fmt-check
make lint
make test
make test-integration
make auth-test
make validate
make run-demo
make literature-demo
make experiment-demo
make full-research-demo
make local-model-demo
make web-lint
make web-test
make browser-test
make compose-test
make native-kimi-test
```

The canonical CI workflow runs `go vet`, `go test -race -shuffle=on ./...`, integration tests, Python Ruff checks, and pytest when those components exist. A separate deployment-authentication job runs `make auth-test`; the browser, Compose, and native-Kimi jobs retain their own acceptance targets.

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
- Operator-token generation, exactly-one-source loading, canonical encoding, private regular files, UID/mode rejection, and non-disclosure.
- API contracts, mandatory Bearer authentication before routing/body work, exact health exemptions, and authenticated CLI REST/polling/SSE without credential-forwarding redirects.
- Python control-center behavior, throttled login, finite server-side sessions, private storage, exact Host/Origin controls, callback/client/SSE revocation, and backend-only bearer forwarding.
- Headless Chromium acceptance for operator login/logout, token non-disclosure, revoked-client behavior, live events, graph, Plotly, experiments, fleet links, and persistent chat.
- Docker Compose config/build/start smoke, operator-secret bootstrap isolation, loopback bindings, and an actual authenticated research run through the published API.
- Real native Kimi session/workspace isolation and private token handoff without OAuth or model prompts, including authenticated daemon CLI access and rejection of an unauthenticated API request.

## Operator authentication checks

`make auth-test` is the focused deployment/script check. It runs:

1. `scripts/tests/operator_auth_test.sh`: existing-file reuse, missing-file failure, ephemeral generation and cleanup, generator selection, token non-disclosure, and `0400`/`0600`, symlink, mode, and ownership checks.
2. `scripts/tests/compose_auth_test.sh`: required file-backed Compose secret, the one-shot UID `65532`/`0400` bootstrap, read-only mounts only for `researchd` and `ui`, non-root service boundaries, public health checks, exact loopback UI settings, and private NiceGUI tmpfs storage.
3. `scripts/tests/script_auth_static_test.sh`: authentication setup/cleanup in demo and smoke wrappers, systemd/environment examples, credential-ignore rules, CI wiring, and health-check usage.
4. `tests/local_model_cache_test.sh`: the cache failure/download short-circuits with operator authentication enabled and without downloading a model or starting services.

This target does not replace the Go/Python suites or live browser/Compose acceptance. Authentication coverage is divided as follows:

| Target | Authentication coverage |
| --- | --- |
| `make test` | Go unit tests for 256-bit generation and no-overwrite behavior, strict token parsing/file ownership/modes, constant-time digest comparison, CLI bearer attachment and offline `init`/`validate`, exact `GET /healthz`, and authentication of every API/metrics/unknown route before body processing. |
| `make test-integration` | Real daemon fail-closed startup before state/listener creation, public health versus protected routes, wrong/malformed/duplicate credentials, authenticated daemon/CLI research and recovery, and REST/SSE redirect refusal without token leakage. |
| `make web-test` | UI credential loading and fixed-length comparison, login body bounds and throttling, accessible fixed-redirect login, finite server-side cookie/session attributes, independent startup-secret invalidation, Host/Origin/WebSocket enforcement, expiry/logout callback and client revocation, SSE cancellation, and backend bearer/cursor validation. |
| `make browser-test` | Real Chromium anonymous redirect, failed and successful login, password-field behavior, no token in cookies or Web Storage, authenticated dashboards/chat, logout revocation of another connected client, and protected UI/API proxy responses. |
| `make compose-test` | Real Compose startup through the short-lived secret bootstrap, loopback health checks, and an authenticated research run using generated ephemeral credentials by default. |
| `make native-kimi-test` | Native transport isolation plus authenticated daemon CLI operations and an explicit unauthenticated API `401`, without OAuth or model prompts. |

Run the targets relevant to a change; a documentation update alone is not evidence that this acceptance matrix has passed.

## Integration recovery test

Two process-level tests exercise recovery. The runtime helper commits one result, blocks on a second task, receives `SIGKILL`, and resumes in a new process. The daemon acceptance test starts the actual `researchd` with `AUTORESEARCH_DEMO_DELAY=2s` and a generated owner-only operator token file for deterministic fault timing and authenticated access, kills it with `SIGKILL`, restarts it on the same private data directory, and verifies three unique completed tasks/findings/messages, an artifact, ordered events, and no open budget reservations.

## Demonstration

The deterministic demos exercise delegation, criticism, budget accounting, durable events, literature, experiments, and final reports. `full-research-demo` combines literature, hypotheses, sandboxed execution, verification, and playback in one session. `local-model-demo` downloads and SHA-verifies Qwen GGUF, runs llama.cpp, and proves a real autonomous run with bounded HF narration and chat. These runs require local operator authentication but no external provider credentials; their wrappers generate ephemeral credentials by default rather than disabling authentication.
