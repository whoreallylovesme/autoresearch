# Development Progress

Last updated: 2026-11-25

## Current status

Milestones 0 and 1 are complete. Milestone 2 (persistence and reliability) is next.

## Implemented and verified

- Public GitHub repository, English architecture/documentation set, ADRs, MIT license, CI, configuration examples, and role prompts.
- Strict YAML decoding with unknown-field/duplicate-key rejection, relative reference resolution, semantic tool/model/budget validation, circular-reference detection, and SHA-256 snapshots.
- `Provider` abstraction, deterministic `MockProvider`, and OpenAI-compatible HTTP/SSE transport with tools, usage, cancellation, rate limits, classified errors, and bounded retries.
- Concurrent budget admission with reservations, one-time reconciliation, conservative interrupted-request accounting, independent provider quotas, and finalization reserve.
- Priority/dependency scheduler with backpressure, retries, cancellation, leases, fencing tokens, expiry recovery, and duplicate idempotency-key rejection.
- Structured message envelopes and a bounded native agent tool-call loop with role allowlists and malformed-call handling.
- Autonomous Chief Scientist demo delegating two research tasks and a dependent independent critic, aggregating durable-contract findings in memory, and producing a limitations-aware report.
- Versioned REST/SSE API, `researchd`, `researchctl`, graceful server controls, and a credential-free process-level integration demo.

## Verification evidence

On the remote VPS, `make fmt-check`, `make lint`, `make test`, `make test-integration`, `make validate`, and `make run-demo` are the required checks. The integration test builds both binaries, starts a real daemon on a free loopback port, operates it through the CLI, and verifies the final report. Unit tests run with Go race detection and use no paid calls.

## Architectural decisions

See `docs/adr/`. Real provider accounting additionally requires configured per-million-token pricing and maximum input estimates. Deterministic tests are deliberately separate from the planned local Hugging Face inference smoke test.

## Known limitations

- Research state is in memory; daemon restart does not yet resume a run.
- Lease expiry recovery is implemented in the scheduler but is not yet wired to persistent heartbeats and process recovery.
- Literature, research graph, experiments, sandboxing, NiceGUI, and playback are not implemented.
- Several configured tool names are reserved and return an explicit minimal-runtime error rather than pretending to execute.
- `pause`, `resume`, `finalize`, and `chat` CLI commands are reserved but unavailable.
- Accounted streaming inside the research runtime is unavailable; the underlying provider streaming contract is implemented and tested.
- Real providers have only local HTTP contract tests; no paid live call has been attempted.

## Blockers

None for Milestone 2. Real provider use requires operator credentials and an explicit test budget. External agent adapters require separate official-protocol investigation.

## Next implementation steps

1. Add SQLite migrations, WAL configuration, event/task/message/budget persistence, and artifact storage.
2. Move scheduler and runtime mutations into transactional storage with consistent checkpoints.
3. Add heartbeats, lease recovery, graceful restart, and a forced-`SIGKILL` integration test.
4. Implement operator pause/resume/finalize controls after durable state exists.

## Next handoff

Work on the remote host. The integrated project is in `/opt/autoresearch`; milestone worktrees are siblings under `/opt`. Read this file, the roadmap, ADRs, and implementation plans. Do not claim later milestones from their interfaces alone. Run every check on the VPS and keep secrets and real infrastructure addresses out of Git.
