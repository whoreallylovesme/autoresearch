# AutoResearch

AutoResearch is an open-source runtime for long-running, multi-agent scientific research. A configurable Chief Scientist coordinates replaceable research, development, brainstorming, and criticism agents while deterministic Go infrastructure enforces budgets, scheduling, isolation, verification, and auditability.

**Current status:** Milestones 1–3 (autonomous runtime, crash-safe persistence, and scientific knowledge/research graph) are implemented and verified. Later milestones remain active or planned. The [progress log](docs/progress.md) is the source of truth; planned features are not presented as available.

## Design principles

- **Deterministic control, nondeterministic reasoning.** Models propose actions; the runtime authorizes them.
- **Evidence over claims.** Verification requires machine-checkable evidence, not model confidence.
- **Durable research state.** Events, tasks, messages, artifacts, and the research graph survive restarts.
- **Autonomy inside explicit boundaries.** Routine work needs no approval; policy changes and external effects do.
- **Reproducibility by default.** Experiments retain revisions, configurations, logs, checksums, and negative results.

## Architecture

```mermaid
flowchart LR
    Chief[Chief Scientist] --> Runtime[Deterministic Go runtime]
    Workers[Configurable agent fleet] --> Runtime
    Runtime --> Providers[Model providers]
    Runtime --> Store[(SQLite + artifacts)]
    Runtime --> Executors[Isolated executors]
    UI[Python control center] --> API[Versioned REST + SSE API]
    CLI[researchctl] --> API
    API --> Runtime
```

The backend is Go. The control center is Python. SQLite WAL, filesystem artifacts, and a single-node deployment keep the initial system understandable and operable on an ordinary VPS.

## Quick start

Requires Go 1.24 and Python 3 for the demo process wrapper. No API credentials or GPU are needed.

```bash
make validate
make test
make test-integration
make run-demo
make literature-demo
```

The demo builds `researchd` and `researchctl`, starts a loopback API, and completes an autonomous Chief Scientist run with two research workers and independent criticism. To operate a daemon manually:

```bash
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
./bin/researchd --config configs/autoresearch.yaml
# In another terminal:
./bin/researchctl run --config configs/autoresearch.yaml
./bin/researchctl status
./bin/researchctl agents
./bin/researchctl tasks
./bin/researchctl budget
./bin/researchctl logs --follow
./bin/researchctl report
```

Research state uses SQLite WAL, transactional events/snapshots, durable messages, budget snapshots, and checksummed artifacts. Forced-termination recovery is tested against the real daemon. The scientific layer includes a revisioned research graph, verifier-gated hypotheses, FTS5 paper/claim search, a deduplicating arXiv/PDF fetcher, and a cited literature-review demo. Sandboxed experiments and the web control center belong to later milestones. `pause`, `resume`, `finalize`, and `chat` are reserved CLI commands but are not yet implemented. See [deployment](docs/deployment.md), [configuration](docs/configuration.md), and the [roadmap](docs/roadmap.md).

## Documentation

Start with the [vision](docs/vision.md), [architecture](docs/architecture.md), and [security model](docs/security.md). Important decisions are recorded in [ADRs](docs/adr/). Development coordination lives in [docs/progress.md](docs/progress.md).

## Contributing and security

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Never submit credentials, private datasets, private research, or real infrastructure addresses. Report security concerns according to [SECURITY.md](SECURITY.md).

## License

AutoResearch is available under the [MIT License](LICENSE).
