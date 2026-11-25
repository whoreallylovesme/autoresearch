# Event Store and Persistence

## Contract

Every event contains a stable UUID, run ID, monotonically increasing per-run sequence, UTC timestamp, event type, schema version, actor, optional correlation and causation IDs, and structured JSON payload. The per-run sequence is the UI and replay ordering key; wall time is not unique.

Events are append-only. Payload schemas are versioned and decoded strictly for runtime consumers. Sensitive values, authorization headers, and raw secret-bearing configuration are redacted before persistence.

## Transaction model

A state transition, its outbox/message changes, budget accounting, and corresponding events commit in one SQLite transaction. The API streams committed events by sequence and supports resume with `Last-Event-ID`. A client reconnects with its last sequence rather than relying on a lossy in-memory channel.

## Snapshots versus replay

Current agent, task, message, graph, budget, and experiment tables are transactional snapshots optimized for queries and scheduling. The event log provides audit, replay, debugging, and metrics. Full event sourcing is intentionally avoided where it would complicate correctness without benefit.

## SQLite

Migrations are ordered, transactional, and recorded in a schema table. WAL mode, foreign keys, busy timeout, and a single process writer are required. Checkpoints coordinate SQLite backup with artifact manifests. Filesystem artifacts are content-addressed and checksummed; database references become visible only after durable file writes.

## Recovery

Graceful shutdown cancels contexts, stops new leases, waits for bounded cleanup, and checkpoints. Forced termination tests verify that committed work remains, partial reservations follow recovery policy, stale leases are fenced, and uncommitted transitions disappear atomically.

## Event families

Research, agent, task, hypothesis, paper, experiment, budget, human, executor, verification, and artifact events use dot-separated names such as `task.completed` and `budget.threshold_reached`. Unknown event types remain preserved for forward-compatible audit even if an older UI cannot render them richly.
