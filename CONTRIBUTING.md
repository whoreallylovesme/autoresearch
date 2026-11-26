# Contributing

AutoResearch favors small, tested, end-to-end changes over speculative abstractions.

Independent work may be delegated freely using role-based agents, AgentSwarm, Tower, or larger multi-wave workflows. There is no project-specific headcount or workflow-size limit; platform and security boundaries still apply. See `docs/multi-agent-development.md` for partitioning, integration, and verification conventions.

1. Read `docs/architecture.md`, relevant ADRs, and `docs/progress.md`.
2. Keep development and test execution on the designated development machine.
3. Write a failing test for new behavior, then implement the smallest coherent change.
4. Run `make fmt-check lint test` and the credential-free integration demo.
5. Update documentation in the same change, especially the progress log and roadmap status.
6. Commit in English with a descriptive conventional-commit-style subject.

Do not commit secrets, infrastructure addresses, private research materials, generated model weights, or experimental artifacts. Do not spend paid API credits in automated tests. Use deterministic mocks.

For security-sensitive changes, document the trust boundary, failure mode, resource limits, and recovery behavior. Architecture changes require an ADR.
