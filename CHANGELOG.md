# Changelog

All notable changes are documented here. The project follows Semantic Versioning once a stable release is published.

## [Unreleased]

### Added

- Initial project vision, architecture, configuration specification, security boundaries, ADRs, roadmap, and continuous-integration scaffolding.
- Strict versioned YAML loading with reference validation and SHA-256 configuration snapshots.
- Deterministic mock and OpenAI-compatible providers with tools, streaming, retries, cancellation, and usage.
- Concurrent budget reservations, provider quotas, and finalization reserves.
- Native Chief Scientist/worker loop, leased scheduler, structured messages, findings, criticism, and final reports.
- Versioned REST/SSE API, independently deployable daemon, CLI, and credential-free end-to-end demo.
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
