# Development Progress

Last updated: 2026-11-25

## Current status

Milestones 0–5 are complete. Milestone 6 (full autonomous research and hardening) is active.

## Implemented and verified

### Runtime and scientific infrastructure

- Strict YAML, native agents, provider abstraction, concurrent budgets, scheduler leases, persistent messages/events, and crash recovery.
- Research graph, verifier-gated hypotheses, FTS5 knowledge, cached arXiv/PDF processing, and cited literature review.
- Restricted Docker and SSH execution, Git worktrees, benchmark/custom-command verification, reproducibility records, and an autonomous verified computational experiment.

### Python control center

- NiceGUI overview with objective, lifecycle, agents, tasks, experiments, papers, hypotheses, and budget metrics.
- Agent fleet and individual agent detail pages with models, providers, states, heartbeats, usage, tasks, and event history.
- Cytoscape.js graph with pan/zoom, clickable nodes, status/author/timeline filters, live refresh, and an accessible semantic node table.
- Plotly provider accounting and agent/model token tables; unavailable time series is explicitly marked unavailable.
- Experiment dashboard with status, executor, metrics, verification, logs, and artifacts.
- Resumable live event stream with actor/type filters, reconnection, visibility-aware resource saving, and live-state indicator.
- Report view, persistent Chief Scientist chat, authoritative directive envelopes, and start/pause/resume/finalize/emergency-stop controls.
- Same-origin runtime proxy, loopback-only defaults, pinned local Cytoscape assets, responsive tables, keyboard-scrollable regions, focus indicators, and touch-sized controls.

## Verification evidence

- Go: `make fmt-check`, `make lint`, `make test`, `make test-integration`, `make validate`.
- Acceptance demos: `make run-demo`, `make literature-demo`, `make experiment-demo`.
- Python: `make web-lint`, `make web-test`.
- Browser: `make browser-test` starts fresh `researchd` and NiceGUI processes, runs the experiment, and verifies live events, Cytoscape graph, Plotly, experiments, fleet links, and persistent chat in Chromium.
- Forced daemon recovery, real Docker execution, and race detection are exercised on the VPS. No paid API or GPU is used.

## Known limitations

- Research playback and advanced long-horizon context compaction are not implemented.
- The local Hugging Face/llama.cpp inference smoke test is not yet deployed.
- Development uses root-managed Docker; rootless deployment remains hardening work. SSH uses local fixtures, and Slurm is not implemented.
- Real providers have local contract tests only; no paid live call has been attempted.
- The UI is loopback-only by design. Remote exposure requires authenticated TLS infrastructure.

## Blockers

None for Milestone 6. A small Qwen GGUF model and llama.cpp can run on the VPS CPU without paid credentials.

## Next implementation steps

1. Deploy pinned llama.cpp and Qwen2.5 0.5B GGUF, then verify the OpenAI-compatible local provider end to end.
2. Add research playback from stored event sequences.
3. Harden hierarchical context summaries, provider quotas, long-horizon finalization, and deployment templates.
4. Complete full-stack verification, CI, and public release documentation.

## Next handoff

Use `/opt/autoresearch` on the remote VPS and sibling milestone worktrees. Run all builds, model inference, and browser tests on that host. Keep secrets and private research data outside Git and read this file plus the roadmap before continuing.
