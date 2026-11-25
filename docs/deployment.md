# Deployment

## Target

The reference target is a Linux VPS with 8 vCPU, 32 GB RAM, NVMe storage, no local GPU, systemd, and optional Docker. Remote SSH/Slurm resources are opt-in. No real hostnames, addresses, credentials, or private research paths belong in this repository.

## Docker Compose

The reviewed Compose deployment builds a multi-stage Go/Python image and starts llama.cpp, `researchd`, and NiceGUI without mounting the host Docker socket. Run `docker compose -f deploy/docker-compose.yml up --build`. The UI is published at `http://127.0.0.1:8081`, the API at `127.0.0.1:8080`, and local inference at `127.0.0.1:8082`; all host bindings remain loopback-only. Use `AUTORESEARCH_MODEL_DIR` to select the external model directory and `docker compose -f deploy/docker-compose.yml down` to stop services. The default config avoids Docker-in-Docker execution; run experimental executors on the host or configure a separate rootless executor explicitly.

## Layout

- Source: an operator-selected directory such as `/opt/autoresearch`.
- Private state: an operator-selected data directory outside the public repository or an ignored `data/` directory.
- Public repository remote: a dedicated least-privilege deploy key.
- API: `127.0.0.1:8080` by default; expose only through authenticated TLS infrastructure.
- Local inference: loopback-only OpenAI-compatible llama.cpp service on a separate port.
- PDF extraction: `poppler-utils` (`pdftotext`) on the daemon host; subprocesses receive no secret environment.

## Build and run

```bash
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
uv sync --frozen --project web
researchctl validate --config configs/autoresearch.yaml
researchd --config configs/autoresearch.yaml --data-dir /srv/autoresearch-private
```

Use a dedicated unprivileged service account for production. Run `make literature-demo` and `make experiment-demo` for the scientific acceptance scenarios. Run `make control-center` to build and start both `researchd` on `127.0.0.1:8080` and NiceGUI on `127.0.0.1:8081`, then execute the configured research run. `make browser-test` performs the Chromium acceptance suite. `deploy/` contains service templates once daemon behavior is implemented; rendered secret-bearing unit files are not committed.

## Local Hugging Face inference

The experimental demo uses `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`; pull it before running `make experiment-demo`. The local model is `Qwen/Qwen2.5-0.5B-Instruct-GGUF` (q4_k_m, SHA-256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`), served by `ghcr.io/ggml-org/llama.cpp@sha256:b0cd5490eefff30b40dcd47f7249a7d7211136908f73fe440a2a86b35a22b23b`. `make local-model-demo` verifies both artifacts and uses `--ctx-size 8192 --parallel 2` for 4096 tokens per slot. Model files live outside Git. A small model is useful for transport and bounded narration, not high-stakes scientific correctness.

## Backup and upgrade

Stop new work or use a consistent SQLite backup, include artifact manifests and configuration snapshots, verify checksums, then apply migrations. Keep the previous binary for rollback. Never copy live SQLite files without a SQLite-aware consistent backup procedure.

## systemd templates

`deploy/researchd.service`, `deploy/autoresearch-ui.service`, and `deploy/autoresearch-llama.service` are reviewed templates, not enabled services. Adjust service users and private paths before installation. Do not expose loopback services publicly without authenticated TLS.

## Emergency stop

Use `researchctl stop`, the web control, or systemd stop. The daemon rejects new admissions, cancels jobs where possible, persists state, and reports unreconciled remote work honestly.
