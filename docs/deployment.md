# Deployment

## Target

The reference target is a Linux VPS with 8 vCPU, 32 GB RAM, NVMe storage, no local GPU, systemd, and optional Docker. Remote SSH/Slurm resources are opt-in. No real hostnames, addresses, credentials, or private research paths belong in this repository.

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
researchctl validate --config configs/autoresearch.yaml
researchd --config configs/autoresearch.yaml --data-dir /srv/autoresearch-private
```

Use a dedicated unprivileged service account for production. Run `make literature-demo` and `make experiment-demo` for the scientific acceptance scenarios. Run `make control-center` to build and start both `researchd` on `127.0.0.1:8080` and NiceGUI on `127.0.0.1:8081`, then execute the configured research run. `make browser-test` performs the Chromium acceptance suite. `deploy/` contains service templates once daemon behavior is implemented; rendered secret-bearing unit files are not committed.

## Local Hugging Face inference

The experimental demo uses `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`; pull it before running `make experiment-demo`. The lightweight local smoke-test model is `Qwen/Qwen2.5-0.5B-Instruct-GGUF`, served by llama.cpp with CPU inference. Model files live outside Git. Pin the server revision and model SHA-256 in a real deployment. A small model is useful for transport and lifecycle testing, not for high-stakes scientific correctness.

## Backup and upgrade

Stop new work or use a consistent SQLite backup, include artifact manifests and configuration snapshots, verify checksums, then apply migrations. Keep the previous binary for rollback. Never copy live SQLite files without a SQLite-aware consistent backup procedure.

## Emergency stop

Use `researchctl stop`, the web control, or systemd stop. The daemon rejects new admissions, cancels jobs where possible, persists state, and reports unreconciled remote work honestly.
