# Deployment

## Target

The reference target is a Linux VPS with 8 vCPU, 32 GB RAM, NVMe storage, no local GPU, systemd, and optional Docker. Remote SSH/Slurm resources are opt-in. No real hostnames, addresses, credentials, or private research paths belong in this repository.

AutoResearch requires its single operator credential on loopback as well as remote deployments. Provision it before starting `researchd` or the UI; do not bypass authentication for health checks, local demos, or container-to-container traffic. The only credential-free commands are local operations such as `researchctl init`, `researchctl validate`, and `researchctl auth generate`.

## Docker Compose

The reviewed Compose deployment builds a multi-stage Go/Python image and starts llama.cpp, `researchd`, and NiceGUI without mounting the host Docker socket. The UI is published at `http://127.0.0.1:8081`, the API at `127.0.0.1:8080`, and local inference at `127.0.0.1:8082`; all host bindings remain loopback-only. Container listeners accept internal Compose traffic, but those listeners are not public host bindings.

### Provision the Compose operator secret

Choose a private source file outside the repository and export its path. Create it as the operator who will invoke Compose, using a private writable parent directory:

```bash
researchctl auth generate --file /srv/autoresearch-secrets/operator.token
chmod 0400 /srv/autoresearch-secrets/operator.token
export AUTORESEARCH_OPERATOR_TOKEN_FILE=/srv/autoresearch-secrets/operator.token
unset AUTORESEARCH_OPERATOR_TOKEN
docker compose -f deploy/docker-compose.yml up --build
```

Compose fails configuration if `AUTORESEARCH_OPERATOR_TOKEN_FILE` is unset. The file-backed Compose secret is mounted only into `operator-auth-init`. That short-lived, non-restarting bootstrap:

1. prepares `/run/autoresearch` in the dedicated `operator-auth` volume as UID `65532`, mode `0700`;
2. copies the source to `/run/autoresearch/operator.token`;
3. sets the copy to UID/GID `65532`, mode `0400`; and
4. exits successfully before `researchd` and `ui` start.

The initializer has a read-only root filesystem, no published ports, all capabilities dropped except the narrowly required `CHOWN`, `DAC_OVERRIDE`, and `FOWNER`, and no operator token in its environment. Among long-running services, only `researchd` and `ui` mount `operator-auth`, both read-only at `/run/autoresearch`. Both run as UID `65532`, satisfying the effective-UID ownership check. Llama.cpp, native Kimi services, and other workloads neither mount this volume nor receive `AUTORESEARCH_OPERATOR_TOKEN` or its file path.

The UI stores NiceGUI state at `/tmp/nicegui` on its own UID-`65532`, mode-`0700` tmpfs. Compose health checks use only the public `GET /healthz` API endpoint and `GET /health` UI endpoint. The UI health checker supplies `Host: localhost:8081`, which is why the default Host allowlist includes that value.

Use `AUTORESEARCH_MODEL_DIR` to select the external model directory and `docker compose -f deploy/docker-compose.yml down` to stop services. Do not use `down --volumes` for a routine restart: it destroys private state and the provisioned token volume. The default config avoids Docker-in-Docker execution; run experimental executors on the host or configure a separate rootless executor explicitly.

Repository scripts such as `scripts/compose-smoke.sh` and `scripts/control-center.sh` reuse a valid `AUTORESEARCH_OPERATOR_TOKEN_FILE` or create an isolated temporary token for the script lifetime. They remove an inline token variable, validate ownership and mode, never print token contents, and clean up only token files they generated. `make control-center` prints the UI URL and token-file path for login, not the token itself.

### Remote UI and API

Keep the default host port bindings on loopback and place a trusted HTTPS reverse proxy on the host where remote access is required. Do not expose the plain HTTP container or daemon listeners directly to an untrusted network. The proxy must preserve the exact public Host, and browser Origin entries must exactly match the HTTPS origin, including any non-default port.

For Compose, use an operator-maintained override outside the public repository to replace the UI allowlists. For example:

```yaml
services:
  ui:
    environment:
      AUTORESEARCH_UI_ALLOWED_HOSTS: "localhost:8081,research.example.test"
      AUTORESEARCH_UI_ALLOWED_ORIGINS: "https://research.example.test"
      AUTORESEARCH_UI_SESSION_MAX_AGE: "28800"
```

Retain `localhost:8081` for the supplied container health check. Access the UI only as `https://research.example.test` in this example. Non-loopback HTTP origins are rejected at UI startup, and HTTPS origins enable Secure cookies. The UI still requires the operator token; TLS and Host/Origin allowlists do not replace authentication.

For direct remote CLI/API access, terminate HTTPS at a separately controlled endpoint and point `researchctl --api` at its final HTTPS URL. The daemon's `--allow-non-loopback` option only permits a non-loopback listener; it neither supplies TLS nor relaxes Bearer authentication. Apply exact network allowlists at the proxy/firewall and keep the daemon's native listener private where possible. The CLI refuses to follow credential-forwarding redirects.

### Compose rotation

The current architecture accepts one operator token, not a generation list or hot reload. Rotation is a planned restart operation:

1. Generate a replacement at a new path; `auth generate` deliberately refuses to overwrite the old file.
2. Point `AUTORESEARCH_OPERATOR_TOKEN_FILE` at that replacement.
3. Force the one-shot initializer to run again so it copies the replacement into the existing private volume.
4. Recreate both consumers so the daemon loads the new API credential and the UI loads the new upstream credential and generates a new session-signing secret.

For example:

```bash
researchctl auth generate --file /srv/autoresearch-secrets/operator-next.token
chmod 0400 /srv/autoresearch-secrets/operator-next.token
export AUTORESEARCH_OPERATOR_TOKEN_FILE=/srv/autoresearch-secrets/operator-next.token
unset AUTORESEARCH_OPERATOR_TOKEN
docker compose -f deploy/docker-compose.yml run --rm --no-deps operator-auth-init
docker compose -f deploy/docker-compose.yml up -d --force-recreate --no-deps researchd ui
```

Update any host or per-user CLI token copies as part of the same procedure. After both restarts, the old API Bearer token fails and old UI cookies cannot restore a session. Operators must sign in with the new token. Restarting only the daemon leaves the UI with the wrong upstream token; restarting only the UI leaves the daemon on the old API token. An ordinary UI restart also invalidates sessions because its signing secret is intentionally ephemeral.

### Optional native Kimi profile

The `kimi-web` profile adds the pinned official Kimi Code 2.8.0 server, a one-shot volume-permission initializer, and a non-root token relay. Only the native server token crosses into `researchd`, through the separate read-only `kimi-web-token` volume; the Kimi home directory and OAuth credentials remain in the server volume. Neither that token nor the native OAuth flow is the AutoResearch operator credential. The UI does not receive the native token.

Long-running native services have no Linux capabilities. The one-shot initializer recursively migrates ownership only inside the dedicated native home/session volumes so existing root-owned files remain readable after the move to UID `65532`; a legacy `server.token` is also tightened to mode `0600` before the native server starts. Native model prompts are never submitted by deployment or smoke tests.

```bash
export AUTORESEARCH_CONFIG=configs/kimi-web-demo.yaml
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web up -d --build
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web exec -it kimi-web kimi login
```

`AUTORESEARCH_OPERATOR_TOKEN_FILE` must remain set to the separately provisioned operator source while using this profile. OAuth is an explicit interactive operator action. It is distinct from `MOONSHOT_API_KEY` used by `provider: kimi` and does not provide AutoResearch OIDC/operator login.

When upgrading an existing native profile, run the one-shot migration explicitly before bringing up the rest of the stack; Compose may otherwise keep the previously completed init container:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web up -d --build --force-recreate --no-deps kimi-web-init
docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web up -d --build
```

Inspect the authenticated live `/api/v1/models`, `/openapi.json`, and `/asyncapi.json` when selecting model capabilities; KAP is experimental and versioned independently of this project. The default `configs/kimi-web-demo.yaml` uses a mock Chief and native workers, so `researchctl run` can consume subscription-backed generation. Use the mixed-fleet example only after configuring the real Chief and budget.

The native server listens on the Compose network and a loopback host port, by default `127.0.0.1:58627`. `--allowed-host kimi-web` permits only that additional internal Host header, and native bearer authentication remains enabled. Server logs are silent by default to avoid accidental token disclosure. Native token rotation is relayed atomically, but existing provider instances retain their original token; recreate `researchd` after native rotation. This native relay behavior does not hot-rotate the AutoResearch operator token, which still requires the full procedure above. Runtime restart recovery starts fresh native sessions while retaining durable runtime state and the old sessions in Kimi's private history.

`make native-kimi-test` uses an isolated Compose project and generated operator token, checks non-root execution and token modes, validates and starts a native-configured daemon, and creates two real sessions with distinct workspaces. It does not start a research run or send model prompts and removes its test containers and volumes afterwards.

## Layout

- Source: an operator-selected directory such as `/opt/autoresearch`.
- Private state: an operator-selected data directory outside the public repository or an ignored `data/` directory.
- Operator and provider credentials: private files outside the source and Docker build context, with no copies in snapshots or artifacts.
- Public repository remote: a dedicated least-privilege deploy key.
- API: authenticated `127.0.0.1:8080` by default; expose remote access only through authenticated HTTPS infrastructure.
- UI: authenticated `127.0.0.1:8081` by default, with private NiceGUI server-side storage.
- Local inference: loopback-only OpenAI-compatible llama.cpp service on a separate port.
- PDF extraction: `poppler-utils` (`pdftotext`) on the daemon host; subprocesses receive no secret environment.

## Build and run

```bash
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
uv sync --frozen --project web
researchctl validate --config configs/autoresearch.yaml
researchctl auth generate --file /etc/autoresearch/operator.token
export AUTORESEARCH_OPERATOR_TOKEN_FILE=/etc/autoresearch/operator.token
unset AUTORESEARCH_OPERATOR_TOKEN
researchd --config configs/autoresearch.yaml --data-dir /srv/autoresearch-private
```

The generation command is a one-time example and requires a private parent directory writable by the invoking user. It fails rather than replacing an existing token. Use a different new path for rotation. Run `researchctl` with a token file owned by the CLI user's effective UID; if that differs from the service UID, provision a separate copy containing the same token.

Use a dedicated unprivileged service account for production. `make control-center` builds and starts both `researchd` on `127.0.0.1:8080` and NiceGUI on `127.0.0.1:8081`, provisions an ephemeral token if needed, and then executes the configured research run. Sign in using the token from the displayed private file path. `make browser-test` performs the Chromium acceptance suite with its own isolated authentication state.

## Secret and build exclusions

Keep real tokens out of the repository, image, command-line arguments, documentation, shell history, logs, configuration snapshots, and research artifacts. `.gitignore` and `.dockerignore` exclude environment files, `*.token`, `*.tokens`, `*.secret`, `*.secrets`, keys, PEM files, `operator-token`, `operator.token`, private/secret directories, runtime data, and `.nicegui` state, including nested Docker-context variants. CI additionally guards common secret filenames.

These exclusions are defense in depth, not permission to place a live secret in the source tree temporarily. Compose's file-backed secret is read at runtime and must not be moved into a Dockerfile `COPY`, `ARG`, committed Compose environment value, or image layer. When building through another system, reproduce equivalent context exclusions and verify the resulting image contains no operator token or NiceGUI session store.

## Local Hugging Face inference

The experimental demo uses `python@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b`; pull it before running `make experiment-demo`. The local model is `Qwen/Qwen2.5-0.5B-Instruct-GGUF` (q4_k_m, SHA-256 `74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db`), served by `ghcr.io/ggml-org/llama.cpp@sha256:b0cd5490eefff30b40dcd47f7249a7d7211136908f73fe440a2a86b35a22b23b`. `make local-model-demo` verifies both artifacts and uses `--ctx-size 8192 --parallel 2` for 4096 tokens per slot. Model files live outside Git. A small model is useful for transport and bounded narration, not high-stakes scientific correctness.

## systemd templates

`deploy/researchd.service`, `deploy/autoresearch-ui.service`, and `deploy/autoresearch-llama.service` are reviewed templates, not enabled services. Both operator-consuming unit templates select `/etc/autoresearch/operator.token` through `AUTORESEARCH_OPERATOR_TOKEN_FILE`; the UI also sets `NICEGUI_STORAGE_PATH=/tmp/nicegui` and uses `PrivateTmp=true`. Adjust service users and private paths before installation.

For a same-UID deployment, add `User=autoresearch` to the `[Service]` section of both the daemon and UI units, then provision the directory and token with that UID:

```bash
install -d -m 0700 -o autoresearch -g autoresearch /etc/autoresearch
sudo -u autoresearch /opt/autoresearch/bin/researchctl auth generate --file /etc/autoresearch/operator.token
chmod 0400 /etc/autoresearch/operator.token
systemctl restart researchd autoresearch-ui
```

The account and group are illustrative; create them using the host's normal service-account procedure before running these commands. The generator must run as the UID that will own the file, not merely as root with a later group-permission workaround. With `PrivateTmp=true`, the UI creates its own UID-owned `/tmp/nicegui` directory and tightens it to `0700`.

If `researchd`, the UI, or a human CLI runs under different UIDs, do not share a root-owned or group-readable file. Give each service its own path in its unit, and provision one regular mode-`0400` or `0600` file per effective UID, owned by that UID and containing the same generated token. A CLI user may therefore need a third copy. Restrict each unit's file read permissions and parent-directory access to that service. Never place the inline token directly in a committed unit or shared environment file.

### systemd rotation

Generate a replacement at a new path, securely install it with the same owner and `0400`/`0600` mode at every configured service and CLI path (or atomically replace a same-UID file), then restart both services:

```bash
systemctl restart researchd autoresearch-ui
```

There is no credential hot reload. Schedule the brief transition so CLI users do not start using the replacement before the daemon has restarted. Verify old API credentials and UI sessions are rejected, then sign in with the replacement. Do not delete the previous source file until every consumer has restarted and the replacement has been verified; securely retire it afterwards according to local secret-handling policy.

Do not expose loopback services publicly without authenticated HTTPS, exact UI Host/Origin allowlists, and trusted network controls.

## Backup and upgrade

Stop new work or use a consistent SQLite backup, include artifact manifests and configuration snapshots, verify checksums, then apply migrations. Keep the previous binary for rollback. Never copy live SQLite files without a SQLite-aware consistent backup procedure. Store credential backups, if required by local policy, separately from ordinary runtime/research backups with equivalent owner-only access. UI restarts intentionally invalidate sessions, so persisted NiceGUI state is not a way to preserve login across upgrades.

## Emergency stop

Use `researchctl stop`, the authenticated web control, or systemd stop. CLI use requires the operator token file even during an emergency; keep a securely accessible current CLI copy as part of operational readiness. The daemon rejects new admissions, cancels jobs where possible, persists state, and reports unreconciled remote work honestly.
