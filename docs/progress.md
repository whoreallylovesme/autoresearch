# Development Progress

Last updated: 2026-11-25

## Current status

All six roadmap milestones are complete and verified on the remote VPS.

## Delivered system

### Autonomous runtime and reliability

- Strict versioned YAML, native configurable agents, replaceable providers, concurrent budget reservations, provider quotas, and scheduler leases with fencing.
- SQLite WAL migrations, transactional snapshots and append-only events, durable messages/budgets, content-addressed artifacts, checkpoints, and forced-`SIGKILL` recovery against the real daemon.
- Graceful shutdown, pause/resume/finalize/stop controls, deterministic priority/cancel directives, and idempotent recovery semantics.

### Scientific research and execution

- Revisioned research graph with typed nodes/edges and verifier-gated hypotheses; FTS5 paper/claim knowledge with source locations and DOI/arXiv deduplication.
- Cached arXiv/PDF fetching with shared downloads, attribution, retries, rate limits, and bounded `pdftotext` extraction.
- Restricted pinned Docker execution, Git worktrees, SSH execution, benchmark/custom-command verification, and durable reproducibility records including failed/refuted outcomes.
- A combined research session that investigates AutoGen and CAMEL, registers two hypotheses, executes and verifies a seeded computational experiment, obtains criticism, finalizes a report, and exposes the full event timeline.

### Control center and local inference

- NiceGUI overview, agent fleet/detail pages, Cytoscape graph, Plotly budget, experiment dashboard, reports, live SSE events, and persistent Chief Scientist chat.
- Event-sequence playback with play/pause/seek/speed and projected task, hypothesis, experiment, and conclusion state.
- Qwen2.5 0.5B GGUF on llama.cpp with pinned image/model hashes. `HFMockProvider` uses deterministic tool plans and bounded local narration; fabricated/source-like narration is rejected in favor of a safe fallback.
- Hierarchical context compaction, shared provider endpoint rate limiting, Prometheus metrics, W3C `traceparent`, and standalone `researchctl init` scaffolding.
- systemd templates for runtime, UI, and llama.cpp; browser and local-model acceptance scripts.

## Verification evidence

All checks run on the VPS without paid API calls or GPU:

- `make fmt-check lint test test-integration validate`
- `make run-demo literature-demo experiment-demo full-research-demo`
- `make local-model-demo`
- `make web-lint web-test browser-test`

The full research test proves two cited papers, proposed and verified hypotheses, a verified Docker experiment, criticism, final report, and playback. The live local-model test proves an autonomous run and chat. Chromium verifies events, graph, Plotly, experiments, playback, fleet links, and persistent chat. Forced daemon recovery and race detection are also green.

## Optional extensions, not roadmap blockers

- Rootless Docker production profiles and authenticated remote UI deployment.
- Paid-provider live conformance tests only with an explicit operator test budget.
- Optional Slurm and additional literature sources.
- Official external agent backends after protocol-specific validation.
- Richer distributed trace spans and optional vector retrieval when justified by measured needs.

## Handoff

The public repository is `https://github.com/whoreallylovesme/autoresearch`. Development and runtime execution were performed on the remote VPS in `/opt/autoresearch` and sibling milestone worktrees. Private credentials and research data are not committed. Use the README quick start and deployment guide; do not treat small-model narration as scientific evidence.
