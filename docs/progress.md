# Development Progress

Last updated: 2026-11-25

## Current status

Milestones 0, 1, and 2 are complete. Milestone 3 (scientific research) is next.

## Implemented and verified

### Runtime and configuration

- Strict versioned YAML loading, semantic validation, relative references, cycle detection, and SHA-256 snapshots.
- Deterministic mock and OpenAI-compatible providers with tools, streaming, retries, cancellation, rate limits, and usage accounting.
- Concurrent budget reservations, reconciliation, provider quotas, finalization reserve, and conservative interrupted-request accounting.
- Native Chief Scientist/workers, role-scoped tools, structured messages, leased scheduler, independent criticism, and final reports.

### Persistence and reliability

- Pure-Go SQLite with WAL, foreign keys, full synchronous durability, ordered migrations, and a single database writer per process.
- Transactional run/task snapshots and append-only events with stable IDs, run IDs, per-run sequences, schema versions, actors, timestamps, and JSON payloads.
- Atomic task completion with finding, message, and event in one commit. Stable event/message IDs make repeated writes idempotent.
- Durable budget snapshots including open reservations; recovery converts interrupted reservations into uncertain usage.
- Content-addressed filesystem artifacts with SHA-256 verification and database metadata, including final reports.
- Scheduler restore with preserved completed results, recovered running tasks, fresh fencing tokens, heartbeats, and duplicate idempotency-key prevention.
- Periodic maintenance, checkpoints, cancellation, graceful daemon shutdown, and resume on restart.

## Verification evidence

The required VPS checks are `make fmt-check`, `make lint`, `make test`, `make test-integration`, `make validate`, and `make run-demo`. Tests use Go race detection and no paid APIs.

Recovery has two independent process-level tests: a controlled runtime helper and the actual `researchd`. Both receive `SIGKILL` after durable work and resume from the same data directory. The daemon test proves three unique completed tasks/findings/messages, one report artifact, ordered event history, and no open reservations after recovery.

## Known limitations

- Literature search, paper fetch/PDF processing, FTS knowledge, and the research graph are not implemented.
- Sandboxed experiments, remote execution, verification backends, and reproducibility records beyond current artifacts are not implemented.
- NiceGUI dashboards, persistent operator chat, and playback are not implemented.
- `pause`, `resume`, `finalize`, and `chat` CLI commands remain reserved but unavailable.
- Several configured tool names return an explicit unavailable error until their milestone is implemented.
- Accounted streaming inside the research runtime remains unavailable; direct provider streaming is implemented and tested.
- Real providers have local HTTP contract tests only; no paid live call has been attempted.

## Blockers

None for Milestone 3. External source adapters must respect source policies and must not treat retrieved content as instructions.

## Next implementation steps

1. Define the persistent research graph with revisioned hypotheses, evidence, edges, and verifier-gated status transitions.
2. Add SQLite FTS5 knowledge tables and the deduplicating Paper Fetcher with source attribution and caching.
3. Implement initial arXiv/OpenAlex-compatible source adapters and structured cited findings without paid model calls.
4. Add critic review workflows and a cited literature-review demonstration.

## Next handoff

Work on the remote host. Integrated source is `/opt/autoresearch`; milestone worktrees are siblings. Private runtime state belongs outside Git. Read this file, roadmap, ADRs, and testing documentation before changing behavior. Never claim verification from model text alone.
