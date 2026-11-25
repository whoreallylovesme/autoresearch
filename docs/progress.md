# Development Progress

Last updated: 2026-11-25

## Current status

Milestones 0–4 are complete. Milestone 5 (web control center) is next.

## Implemented and verified

### Runtime, persistence, and science

- Strict configuration, native agents, replaceable providers, concurrent budgets, scheduler leases, and structured messages.
- SQLite WAL, transactional events/snapshots, durable budgets/messages, artifact integrity, checkpoints, and forced-`SIGKILL` recovery against the real daemon.
- Revisioned research graph, verifier-gated hypotheses, FTS5 knowledge, structured claims, cached arXiv/PDF fetching, and cited literature review.

### Experimental execution

- Docker executor with pinned image allowlists, no network, read-only root, dropped capabilities, no-new-privileges, numeric non-root user, CPU/memory/PID/time limits, and isolated workspace mounts.
- Asynchronous jobs, cancellation, status, streaming logs, exit codes, and bounded artifact collection with symlink/path-traversal protection.
- Git worktree manager that preserves uncommitted work instead of force-deleting it.
- Benchmark and custom-command verifiers, including explicit metric directions and exit-code evidence.
- Experiment records with code/configuration hashes, seeds, hardware, dependencies, commands, logs, metrics, artifacts, and verification outcomes; failed and refuted results persist.
- Autonomous experiment demo that registers a hypothesis, writes and executes seeded Python, verifies `answer=6`, revises the graph, obtains criticism, and persists the verified result after reopen.
- SSH adapter with destination allowlists, BatchMode, strict host-key checking, operator-owned identity files, remote PID/exit records, logs, cancellation, artifact collection, and race-tested concurrent status polling.
- `GET /api/v1/experiments` and `make experiment-demo` acceptance command.

## Verification evidence

Required checks are `make fmt-check`, `make lint`, `make test`, `make test-integration`, `make validate`, `make run-demo`, `make literature-demo`, and `make experiment-demo`. Docker tests execute real containers on the VPS. SSH tests use a local fake transport and do not contact private infrastructure. All Go tests use race detection; no paid API or GPU is used.

## Known limitations

- Development uses root-managed Docker with restricted containers. Rootless Docker and production AppArmor/seccomp profiles remain hardening work.
- SSH is covered by local protocol fixtures, not a real remote-server integration test. Remote cancellation is best-effort. Slurm is not implemented.
- Interrupted Docker/SSH experiments are marked failed on restart rather than transparently resumed; tasks remain at-least-once and require idempotent retry.
- NiceGUI dashboards, persistent operator chat, and playback are not implemented.
- `pause`, `resume`, `finalize`, and `chat` CLI commands remain reserved but unavailable.
- Real model providers have local contract tests only; no paid live call has been attempted.

## Blockers

None for Milestone 5. The runtime exposes versioned REST/SSE contracts for the Python application.

## Next implementation steps

1. Build the NiceGUI control center with overview, agents, budgets, events, experiments, graph, and report views.
2. Add operator stop control and persistent Chief Scientist chat/directives through the same API.
3. Add Cytoscape.js interactive graph and Plotly budget charts with resumable live updates.
4. Add Python tests, deployment configuration, and browser acceptance checks.

## Next handoff

Use the remote VPS for all builds, tests, model serving, and application execution. Integrated source is `/opt/autoresearch`; milestone worktrees are siblings. Keep private runtime state outside Git and do not expose loopback APIs publicly without authentication.
