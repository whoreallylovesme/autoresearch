# Implementation Roadmap

Status legend: **done** means implementation and acceptance checks passed; **active** means partial code may exist; **planned** means no availability is claimed.

| Milestone | Status | Deliverables | Acceptance criterion |
| --- | --- | --- | --- |
| 0. Project initialization | done | Public repository, architecture, configuration specification, ADRs, CI, license | Documentation is complete, inspected, committed, and pushed before implementation |
| 1. Minimal autonomous runtime | done | Go daemon, strict config, provider and backend contracts, deterministic mock, one real transport, native supervisor/workers, delegation, scheduler, messages, budgets, CLI | Chief autonomously delegates to multiple workers and aggregates results without approvals or paid calls |
| 2. Persistence and reliability | done | SQLite migrations, events, durable tasks/messages, checkpoints, heartbeats, recovery, shutdown, cancellation | Killed daemon resumes without losing or duplicating completed work |
| 3. Scientific research | done | Paper Fetcher, literature APIs, PDF processing, FTS knowledge, research graph, evidence, critics | Multiple researchers produce a cited review and persistent graph |
| 4. Experimental execution | done | Sandboxed Docker, worktrees, experiments, artifacts, verifiers, SSH, optional Slurm | Agent proposes, implements, executes, and verifies a reproducible experiment |
| 5. Web control center | done | NiceGUI dashboards, graph, experiments, events, chat, controls | Operator monitors and redirects a live run in a browser |
| 6. Full autonomous research | done | Multiple providers, quotas, compaction, finalization, reports, playback, hardening | Long-running session investigates, experiments, verifies, and reports within limits |

Each milestone requires focused tests, race detection where applicable, security/failure review, documentation updates, a descriptive commit, and a verified push. A later milestone must not be marked done because its types or endpoints merely exist.

## Optional hardening and extensions

The documented roadmap is complete. Future work is prioritized by operator need rather than claimed availability:

1. Rootless Docker production profiles and remote authenticated UI deployment.
2. Paid-provider conformance tests only with an explicit test budget.
3. Optional Slurm, OpenAlex, Semantic Scholar, Crossref, and PubMed adapters.
4. Official ACP/Kimi CLI/Codex/OpenCode backends after protocol-specific validation.
5. Richer trace spans and optional vector retrieval when measured research needs justify them.
