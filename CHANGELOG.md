# Changelog

All notable changes are documented here. The project follows Semantic Versioning once a stable release is published.

## [Unreleased]

### Added

- Initial project vision, architecture, configuration specification, security boundaries, ADRs, roadmap, and continuous-integration scaffolding.
- Strict versioned YAML loading with reference validation and SHA-256 configuration snapshots.
- Deterministic mock and OpenAI-compatible providers with tools, streaming, retries, cancellation, and usage.
- Concurrent budget reservations, provider quotas, and finalization reserves.
- Native Chief Scientist/worker loop, leased scheduler, structured messages, findings, criticism, and final reports.
- Versioned REST/SSE API, independently deployable daemon and CLI, and an end-to-end demo with ephemeral local operator credentials.
- Pure-Go SQLite WAL persistence, migrations, transactional events/snapshots, durable messages, and budget recovery.
- Content-addressed SHA-256 artifact storage with integrity verification.
- Lease heartbeats, checkpoints, restart recovery, and forced-`SIGKILL` tests against both runtime and the real daemon.
- Revisioned research graph with verifier-gated hypotheses and citation edges.
- SQLite FTS5 knowledge base, DOI/arXiv deduplication, structured claims, and source locations.
- Cached arXiv/PDF fetching with rate limits, retries, shared downloads, bounded text extraction, and cited literature review demo.
- Isolated pinned Docker executor with resource limits, streaming logs, cancellation, and artifact collection.
- Git worktree isolation, benchmark/custom-command verification, and durable experiment reproducibility records.
- SSH execution adapter with strict destination/host-key policy and race-tested status polling.
- Autonomous seeded computational experiment with verifier-gated hypothesis evidence.
- NiceGUI research control center with live dashboards, agent details, interactive graph, Plotly, experiments, and reports.
- Resumable live SSE, persistent Chief Scientist chat, and start/pause/resume/finalize/stop controls.
- Python unit tests and repeatable headless Chromium browser acceptance.
- Full literature + experiment research session and event-sequence playback.
- Local Qwen/llama.cpp HFMock provider with bounded, citation-safe narration and live autonomous run.
- Hierarchical context compaction, shared provider rate limits, Prometheus metrics, and W3C trace context.
- CLI project initialization and deterministic operator directives.
- Dedicated Kimi/Moonshot provider with reasoning and JSON mode, plus an OpenAI-Chief/Kimi-worker fleet example and integration test.
- Reproducible Python dependency management with `pyproject.toml`, `uv.lock`, and `uv sync --frozen`.
- Cobra-based daemon/operator CLI and multi-stage Docker Compose deployment for llama.cpp, runtime, and UI.
- Native Kimi Code `kimi web` workers with per-agent KAP sessions/workspaces, incremental history, deterministic runtime tool envelopes, cancellation, and request-only subscription accounting.
- Optional non-root native Kimi Compose profile with an isolated token relay, live session-isolation smoke test, and contributor `AGENTS.md` guidance.
- Mandatory single-operator authentication for the daemon, API CLI, and NiceGUI UI, with machine-generated 256-bit token files, strict UID/mode/regular-file checks, constant-time Bearer verification, and exact public health endpoints.
- Expiring server-side UI sessions with an independent startup signing secret, finite SameSite Strict cookies, exact Host/Origin controls, throttled login, callback/client/SSE revocation, and private NiceGUI storage. This release has one fully privileged operator identity; OIDC and RBAC are not yet implemented.
- Isolated Compose operator-secret bootstrap that copies the token to UID `65532` with mode `0400` in a private volume mounted read-only only by `researchd` and `ui`.
- Focused `make auth-test` deployment checks plus authentication-aware Go process, Python UI, Chromium, Compose, and native-Kimi acceptance coverage.

### Changed

- Consolidated shared ID, retry, capability, pacing, accounting, persistence, artifact, graph, test, and model-bootstrap logic into focused package-local or narrowly scoped helpers.
- Hardened executor Job.ID path validation, pre-read artifact limits, capability immutability, graph snapshot isolation, and budget recovery/persistence-failure handling.
- Unified initial and live web graph conversion, reused canonical Make targets in CI, and removed redundant browser/Compose setup duplication.
- **Operational migration:** existing installations must generate an operator token and configure exactly one of `AUTORESEARCH_OPERATOR_TOKEN` or `AUTORESEARCH_OPERATOR_TOKEN_FILE` for `researchd`, the UI, and API-calling CLI commands. Compose requires a file-backed `AUTORESEARCH_OPERATOR_TOKEN_FILE` setting, and API clients must send `Authorization: Bearer <token>` for every route except exact `GET /healthz`; `/metrics` is no longer public. Operators sign in to the UI with the same token. Offline `init`, `validate`, and `auth generate` remain credential-free, and deterministic demos generate ephemeral local credentials without requiring external provider credentials.
- Token rotation replaces the token file, reruns `operator-auth-init` for Compose, and restarts both `researchd` and the UI. Restarting the daemon rejects the old bearer credential; restarting the UI invalidates existing sessions through its new independent startup signing secret.
