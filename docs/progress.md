# Development Progress

Last updated: 2026-11-25

## Current status

Milestone 0 is complete. Milestone 1 is active; no runtime feature is claimed as implemented yet.

## Completed

- Inspected the remote Linux development host and confirmed 8 vCPU, 32 GiB RAM, Docker, and sufficient storage.
- Created the public GitHub repository using the authenticated account and configured a dedicated repository deploy key on the development host.
- Established an English documentation set covering architecture, configuration, lifecycle, storage, budgets, security, providers, reproducibility, web, testing, deployment, and roadmap.
- Added MIT licensing, contribution/security policies, example configuration, role prompts, CI, and standard development commands.
- Selected deterministic mocks plus a separate small Hugging Face/llama.cpp local inference path; no paid API calls are needed for development.

## Architectural decisions

See `docs/adr/`. The main choices are single-node Go + SQLite WAL, snapshots with append-only events, at-least-once execution with idempotency, separate providers and agent backends, and a Python control center.

## Known bugs

None recorded before implementation.

## Blockers

None for deterministic development. Real provider use requires operator credentials and an explicit test budget. External agent adapters require separate investigation of official protocols.

## Pending tasks

1. Implement strict configuration and the deterministic provider contract.
2. Implement budget reservations, scheduler leases, native agents, and supervisor delegation.
3. Add the daemon/CLI/API and prove the credential-free autonomous demo.
4. Continue through persistence, scientific knowledge, execution, and web milestones.

## Next handoff

Start on the remote host in `/opt/autoresearch`. Read this file, `docs/roadmap.md`, ADRs, and the active implementation plan under `docs/superpowers/plans/`. Run all tests on that host. Commit only after tests pass and update this document with exact remaining work.
