# AutoResearch

AutoResearch is an open-source runtime for long-running, multi-agent scientific research. A configurable Chief Scientist coordinates replaceable research, development, brainstorming, and criticism agents while deterministic Go infrastructure enforces budgets, scheduling, isolation, verification, and auditability.

**Current status:** All six roadmap milestones are implemented and verified, including a combined literature + experiment research session, local Hugging Face inference, and research playback. The [progress log](docs/progress.md) is the source of truth; planned features are not presented as available.

## Design principles

- **Deterministic control, nondeterministic reasoning.** Models propose actions; the runtime authorizes them.
- **Evidence over claims.** Verification requires machine-checkable evidence, not model confidence.
- **Durable research state.** Events, tasks, messages, artifacts, and the research graph survive restarts.
- **Autonomy inside explicit boundaries.** Routine work needs no approval; policy changes and external effects do.
- **Reproducibility by default.** Experiments retain revisions, configurations, logs, checksums, and negative results.

## Docker Compose

Build and start the complete loopback-only stack (llama.cpp, runtime, and control center):

```bash
docker compose -f deploy/docker-compose.yml up --build
docker compose -f deploy/docker-compose.yml exec -T researchd ./bin/researchctl run --config configs/local-hf.yaml --api http://localhost:8080
```

The Compose deployment does not mount the host Docker socket. Use `docker compose -f deploy/docker-compose.yml down` to stop it.

### Native Kimi worker fleet

`provider: kimi-web` connects workers to the official Kimi Code `kimi web` server through its experimental KAP API. Each worker owns a separate provider instance, KAP session, and workspace; a shared server is only the transport, not a shared conversation. The Chief Scientist can independently use OpenAI/Astra or another configured provider.

```bash
export AUTORESEARCH_CONFIG=configs/kimi-web-demo.yaml
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web up -d --build
# Complete OAuth interactively when you are ready to use your own Kimi subscription:
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web exec -it kimi-web kimi login
```

Use `configs/kimi-web-fleet.example.yaml` as the OpenAI-Chief/native-Kimi-worker template and configure its actual model, prices, and quotas before a live run. The default native mode is `manual` for Kimi built-in tools; runtime-authorized tools remain autonomous. `make native-kimi-test` verifies real session isolation and the private token relay without OAuth, model prompts, or paid generation. See [provider integration](docs/provider-integration.md) and [deployment](docs/deployment.md) for limitations.

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

Requires Go 1.24, Python 3.12+, and [`uv`](https://docs.astral.sh/uv/) for the demo process wrapper and control center. No API credentials or GPU are needed.

```bash
make validate
make test
make test-integration
make run-demo
make literature-demo
make experiment-demo
make full-research-demo
make local-model-demo
make browser-test
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
./bin/researchctl chat --message "Summarize current status"
./bin/researchctl pause
./bin/researchctl resume
```

Research state uses SQLite WAL, transactional events/snapshots, durable messages, budget snapshots, and checksummed artifacts. Forced-termination recovery is tested against the real daemon. The scientific layer includes a revisioned research graph, verifier-gated hypotheses, FTS5 paper/claim search, a deduplicating arXiv/PDF fetcher, and a cited literature-review demo. Experimental execution includes pinned Docker isolation, Git worktrees, benchmark/custom-command verification, reproducibility records, and an SSH adapter. The NiceGUI control center provides live dashboards, fleet details, an interactive research graph, budget charts, experiments, events, reports, persistent Chief Scientist chat, and operator controls. Research playback, shared provider rate limiting, bounded context compaction, Prometheus metrics, W3C trace context, and a bounded local Qwen narration provider are also included. Optional production hardening is documented in `docs/progress.md`. See [deployment](docs/deployment.md), [configuration](docs/configuration.md), and the [roadmap](docs/roadmap.md).

## Documentation

Start with the [vision](docs/vision.md), [architecture](docs/architecture.md), and [security model](docs/security.md). Important decisions are recorded in [ADRs](docs/adr/). Development coordination lives in [docs/progress.md](docs/progress.md). Multi-agent delegation patterns and boundaries are documented in [Multi-agent development](docs/multi-agent-development.md).

## Contributing and security

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Never submit credentials, private datasets, private research, or real infrastructure addresses. Report security concerns according to [SECURITY.md](SECURITY.md).

## License

AutoResearch is available under the [MIT License](LICENSE).
