# AutoResearch contributor guide

AutoResearch is a Go scientific-agent runtime with a Python/NiceGUI control center. Keep model nondeterminism outside deterministic scheduler, budget, permission, persistence, and verification policies. Implement the smallest useful vertical slice; do not add placeholder packages for the roadmap.

## Toolchain

- Go 1.24, Cobra for CLI commands, SQLite migrations, and standard `go test` with the race detector.
- Python dependencies are managed only with `uv` using `web/pyproject.toml` and the committed `web/uv.lock`. Do not add requirements files.
- Docker Compose deployment is in `deploy/`; container images and the local GGUF model are pinned. Do not mount the host Docker socket into agent workloads.
- Use the repository Makefile rather than inventing parallel build or test commands.

## Verification

Run `make fmt-check lint test test-integration validate` for Go changes and `make web-lint web-test` for web changes. Run relevant demo, browser, or Compose acceptance tests for end-to-end changes. `make native-kimi-test` must not submit model prompts or require OAuth. Never spend paid API credits in development tests.

Review `git status` and `git diff --check` before committing. Remove generated state and keep credentials, personal files, research artifacts, tokens, and private SSH configuration out of the public repository.

## Architecture invariants

- Provider configuration is separate from agent configuration; prompts, headcounts, tools, and limits remain YAML-driven.
- `provider: kimi` means hosted Moonshot API authentication. `provider: kimi-web` means native Kimi Code through its experimental KAP HTTP API. These are not interchangeable authentication paths.
- Every native Kimi agent has its own provider instance, KAP `session_id`, and `session_workspace/agent_id`. Never route all agents through one shared conversational session or replay an entire conversation into an existing native session.
- A shared `kimi web` process is transport only. Per-agent workspaces and conversation histories remain separate. Persisted runtime task state is authoritative after a restart.
- Native built-in permissions default to `manual`. Auto/yolo modes are currently rejected because the shared native service is not a per-agent filesystem boundary. Runtime tool authorization cannot be delegated to prompts.
- Only the private server token is relayed to `researchd`. The Kimi home directory and OAuth credentials must not be exposed to the daemon. Every native agent receives a separate workspace, but the shared service account is not a filesystem security boundary; native built-in tools therefore remain in manual mode.
- A model statement is not scientific verification. Required verifier evidence, negative results, source attribution, and unknown metrics must remain explicit.

## Documentation and handoff

Keep `docs/` and `CHANGELOG.md` accurate about implemented versus experimental functionality. Record verified changes, blockers, and exact next steps in `docs/progress.md`. Use ADRs for durable architectural tradeoffs. Another coding agent must be able to resume from the repository without this chat history.
