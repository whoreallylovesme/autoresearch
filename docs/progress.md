# Development Progress

Last updated: 2026-11-27

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
- systemd templates for runtime, UI, and llama.cpp; browser and local-model acceptance scripts. Python dependencies are locked and installed with `uv sync --frozen` from `web/pyproject.toml` and `web/uv.lock`.

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

## Provider integration update

A dedicated Kimi/Moonshot adapter and a mixed OpenAI-Chief/Kimi-worker configuration are implemented with fake-endpoint integration tests. The Python environment now uses `uv` and a committed lockfile. CLI parsing uses Cobra, and a multi-stage Docker Compose deployment builds and starts llama.cpp, runtime, and UI without a host Docker socket. No paid Kimi or OpenAI calls have been made.

## Native Kimi Code integration

The `kimi-web` provider uses the official Kimi Code 2.8.0 KAP HTTP API, discovered from the running server's `/openapi.json` and `/api/v1/kap.md`. It creates a distinct native session and workspace per agent, sends incremental runtime messages, correlates replies with `prompt_id`, waits and cancels through official session actions, and exposes runtime tools through a strict JSON envelope. Native built-in approvals/questions are rejected rather than auto-approved. `permission_mode: manual` is currently mandatory because native `auto`/`yolo` would share the service OS identity.

Hosted Moonshot (`provider: kimi`) and native Kimi Code (`provider: kimi-web`) remain separate authentication paths. Native subscription workers use request quotas and zero reported monetary/token usage rather than invented prices; no subscription metering is inferred. The optional Compose profile runs Kimi without Linux capabilities and relays only the private server token to the daemon. OAuth files stay in the native service volume.

Verification includes fake-KAP mixed-fleet execution with an OpenAI Chief and three independent native worker sessions, race-tested cancellation/retry/protocol tests, and `make native-kimi-test` against real `kimi web` in Docker. The latter validates configuration, starts the native-configured daemon and fleet API, checks non-root execution and token permissions, and creates two distinct real session IDs/workspaces without starting research or submitting prompts. Interactive OAuth and live subscription-backed generation have not been performed. Full mixed-fleet generation therefore still requires an explicit operator run after login, live model discovery, and budget configuration.

Contributor handoff rules are in `AGENTS.md`; the CLI uses Cobra, the UI uses `uv`, and both default and native Compose smoke scripts are available.

## Maintenance

Random 128-bit identifiers for messages, research events, native prompts, knowledge claims, and verification records are centralized in `internal/id`. `New` preserves error returns and `MustNew` preserves the existing explicit panic contract. Go package-test placement and the top-level integration-test boundary are documented in `AGENTS.md` and `docs/testing.md`. The refactor passed the full Go race/integration suite, all deterministic research demos, local-model acceptance, and both Compose smoke tests.

Follow-up consolidation centralizes provider retry/capability snapshots, executor artifact limits and Job.ID validation, operator reply persistence, budget account mutations, SQLite entity/event transactions, graph snapshot copying, and provider/literature pacing. Review-found budget recovery and persistence-atomicity defects are fixed with regression coverage rather than preserved as accidental behavior. Browser graph conversion now has one Python owner, test infrastructure reuses bounded process/Docker helpers, CI delegates to canonical Make targets, and model download/checksum bootstrap is shared. All changes passed independent task and whole-branch reviews, full Go/Python verification, browser acceptance with live graph refresh, deterministic demos, local-model execution, and both Compose smoke tests. No paid or subscription-backed prompts were used.

## Handoff

The public repository is `https://github.com/whoreallylovesme/autoresearch`. Development and runtime execution were performed on the remote VPS in `/opt/autoresearch` and sibling milestone worktrees. Private credentials and research data are not committed. Use the README quick start and deployment guide; do not treat small-model narration as scientific evidence.

## Contributor multi-agent workflows

Role-based agents, AgentSwarm, Tower, and multi-wave development workflows are explicitly authorized without a repository-specific headcount or workflow-size limit. `AGENTS.md` assigns integration and safety responsibilities; `docs/multi-agent-development.md` documents concrete AutoResearch examples. Platform limits are stated explicitly rather than hidden: swarm mode is user-controlled and capped at 128 total subagents per invocation, Tower is experimental, explicitly enabled, and mode-exclusive, and resource/security/verification policies remain mandatory.
