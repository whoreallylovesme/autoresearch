# Development Progress

Last updated: 2026-11-25

## Current status

Milestones 0–3 are complete. Milestone 4 (experimental execution) is next.

## Implemented and verified

### Autonomous runtime and reliability

- Strict YAML, replaceable native agents, deterministic and OpenAI-compatible providers, concurrent budgets, priority/dependency scheduling, and role-scoped tools.
- SQLite WAL migrations, transactional task/finding/message/event completion, durable budget recovery, SHA-256 artifacts, heartbeats, checkpoints, and fresh fencing after restart.
- Process-level forced-`SIGKILL` recovery tests for both runtime and the actual `researchd`.

### Scientific research

- Typed research graph nodes/edges with revisions, persistent snapshots, and API access.
- Hypothesis lifecycle where `verified` requires a supported hypothesis plus successful verifier evidence and artifact references.
- Paper metadata, DOI/arXiv normalization and deduplication, paper claims vs interpretations vs verified conclusions, source locations, and negative-result claim types.
- SQLite FTS5 over titles, abstracts, full text, and claims, including persistence after reopen.
- Centralized Paper Fetcher with shared query cache, singleflight search/download, arXiv Atom parsing, attribution, retries, rate limits, and bounded PDF downloads.
- SHA-256 PDF artifacts and isolated `pdftotext` subprocess extraction with timeout, output limits, and no secret environment.
- Deterministic literature review with two researchers, two cited interpretation claims, a hypothesis, independent criticism, a persistent conclusion, and explicit metadata-only limitations.
- `GET /api/v1/graph` and `GET /api/v1/papers` contracts and `make literature-demo` acceptance command.

## Verification evidence

Required VPS checks remain `make fmt-check`, `make lint`, `make test`, `make test-integration`, `make validate`, and `make run-demo`; `make literature-demo` additionally proves the scientific acceptance scenario. Tests use race detection, local HTTP fixtures, a fake PDF extractor command, real SQLite/FTS5, and no paid APIs.

## Known limitations

- The literature demo is metadata-level and does not claim full-text review or scientific effectiveness. Live arXiv retrieval is implemented but covered by local fixtures rather than external integration tests.
- Only arXiv and deterministic static sources are active; OpenAlex, Semantic Scholar, Crossref, and PubMed remain extensions.
- Sandboxed experiments, Git worktrees, verifiers, SSH/Slurm execution, and full experiment reproducibility records are not implemented.
- NiceGUI dashboards, persistent operator chat, and playback are not implemented.
- `pause`, `resume`, `finalize`, and `chat` CLI commands remain reserved but unavailable.
- Real providers have local contract tests only; no paid live call has been attempted.

## Blockers

None for Milestone 4. Docker is available on the development VPS. Remote SSH/Slurm resources require separate explicit configuration and must not receive host credentials by default.

## Next implementation steps

1. Add experiment and verification domain records with immutable logs, metrics, commands, revisions, and artifact links.
2. Implement restricted Docker execution with CPU/memory/PID/time/network/workspace limits and no host socket or secrets.
3. Add Git worktree isolation and unit-test/custom-command/benchmark verifiers.
4. Demonstrate an autonomous reproducible computational experiment, then add explicit SSH execution and optional Slurm adapters.

## Next handoff

Use the remote host and keep private data outside Git. Integrated source is `/opt/autoresearch`; milestone worktrees are siblings. Read the roadmap, security model, testing guide, and this file before implementation. Model claims never constitute experiment verification.
