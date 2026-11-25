# Minimal Autonomous Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove a Chief Scientist can delegate to configurable workers and aggregate a verified result through deterministic, tested runtime infrastructure.

**Architecture:** A Go module implements strict YAML loading, provider and budget decorators, an in-memory leased scheduler, durable-contract message envelopes, native tool-calling agents, and a daemon/CLI/API. The deterministic provider scripts the demo; an OpenAI-compatible HTTP transport proves real-provider integration without requiring paid tests.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, standard-library HTTP/JSON/SSE, SQLite in the following milestone.

## Global Constraints

- All development and execution occur on the remote VPS.
- No paid API calls, credentials, real host addresses, or private materials in tests or Git.
- Models propose actions; runtime tool handlers authorize them.
- Tests use deterministic mocks and `go test -race`.
- Documentation and `docs/progress.md` change with implementation.

---

### Task 1: Strict configuration

**Files:** `go.mod`, `internal/config/types.go`, `internal/config/load.go`, `internal/config/load_test.go`.

**Interfaces:** `Load(path string) (*Config, error)` returns a fully resolved configuration; `Config.Validate() error` rejects invalid headcounts, paths, tools, durations, budgets, and cycles.

- [ ] Write tests for the valid demo, unknown fields, missing prompt/model references, cyclic references, invalid headcount, unsupported model parameters, and secret exclusion from snapshots.
- [ ] Run `go test ./internal/config` and confirm undefined-loader failures.
- [ ] Implement strict YAML nodes with duplicate-key and unknown-field checks, relative path resolution, semantic validation, and SHA-256 canonical snapshots.
- [ ] Run package tests, formatting, vet, and commit `feat: add strict runtime configuration`.

### Task 2: Provider contracts and deterministic mock

**Files:** `internal/provider/types.go`, `internal/provider/mock.go`, `internal/provider/openai.go`, `internal/provider/provider_test.go`.

**Interfaces:** `Provider.Generate`, `Provider.Stream`, `Provider.Capabilities`; `MockProvider` uses a queue of deterministic responses and records requests.

- [ ] Test scripted text/tool responses, cancellation, retry classification, SSE decoding, authentication-header redaction, tool arguments, usage, and unsupported parameters.
- [ ] Implement providers with bounded exponential retry and no secret logging.
- [ ] Run tests and commit `feat: add model provider contracts`.

### Task 3: Concurrent budget manager

**Files:** `internal/budget/manager.go`, `internal/budget/manager_test.go`.

**Interfaces:** `Reserve(ctx, Estimate) (Reservation, error)`, `Reservation.Reconcile(Usage) error`, `Reservation.Release(error)`, and `Manager.Status() Status`.

- [ ] Test exact limits, hundreds of concurrent reservations, exhaustion, finalization reserve, cancellation, unknown usage, and duplicate reconciliation.
- [ ] Implement mutex-protected reservation/reconciliation and explicit estimated/confirmed usage.
- [ ] Run race tests and commit `feat: enforce concurrent research budgets`.

### Task 4: Scheduler and messaging

**Files:** `internal/messaging/types.go`, `internal/scheduler/scheduler.go`, `internal/scheduler/scheduler_test.go`.

**Interfaces:** `Submit(Task) error`, `Lease(ctx, Worker) (*Lease, error)`, `Complete(leaseID, fencingToken, Result) error`, `Recover(now) error`.

- [ ] Test dependencies, priority, duplicate idempotency keys, capacity, cancellation, lease expiry, stale fencing tokens, retry, and persistent message-envelope validation.
- [ ] Implement the smallest correct in-memory scheduler behind storage-friendly interfaces.
- [ ] Run race tests and commit `feat: add leased task scheduler`.

### Task 5: Native agents and supervisor tools

**Files:** `internal/agent/agent.go`, `internal/agent/tools.go`, `internal/research/runtime.go`, related tests.

**Interfaces:** a bounded native provider loop and runtime-owned handlers for task creation/delegation, findings, messages, budget status, and finalization.

- [ ] Test unauthorized tool rejection, malformed arguments, worker result aggregation, cancellation, and deterministic multi-worker delegation.
- [ ] Implement role-configured agents and validated tool handlers without infrastructure privilege escalation.
- [ ] Run tests and commit `feat: implement native research agents`.

### Task 6: Daemon, API, CLI, and demo acceptance

**Files:** `cmd/researchd/main.go`, `cmd/researchctl/main.go`, `internal/api/server.go`, `tests/runtime_integration_test.go`.

**Interfaces:** versioned status/agents/tasks/budget/events endpoints and the specified run/control commands.

- [ ] Write an end-to-end test that runs the deterministic Chief Scientist, asserts multiple worker results and final report, and exercises status and cancellation.
- [ ] Implement graceful context cancellation, SSE scaffolding, and CLI operations against the same API.
- [ ] Run `make fmt-check lint test test-integration validate run-demo` and update README/progress/roadmap.
- [ ] Commit `feat: deliver minimal autonomous research runtime`, inspect `git status`, and push only verified files.
