# Configuration

Configuration is YAML, versioned, strictly decoded, resolved relative to the referring file, and snapshotted before a run. Unknown fields, duplicate mapping keys, invalid references, missing prompts, cyclic includes, unsupported parameters, invalid permissions, impossible budgets, and dependency cycles are startup errors.

Operator authentication is configured through process environment or protected files, never research YAML. Those values are not included in configuration snapshots.

## Operator credential

The daemon, NiceGUI UI, and API-using `researchctl` commands require exactly one of these variables:

| Variable | Purpose |
| --- | --- |
| `AUTORESEARCH_OPERATOR_TOKEN` | Inline operator token. Supported for constrained environments, but prefer the file form where possible. |
| `AUTORESEARCH_OPERATOR_TOKEN_FILE` | Absolute or operator-selected path to a private file containing the token. Preferred for host, systemd, and Compose provisioning. |

Setting both is an error, as is setting neither. An empty value does not provide a usable credential. Do not set the inline variable globally merely to satisfy one process, and do not place either value in research YAML.

### Generate and load a token

Run the generator as the user who will own the new file, with a private parent directory that user can write:

```bash
researchctl auth generate --file /etc/autoresearch/operator.token
chmod 0400 /etc/autoresearch/operator.token
export AUTORESEARCH_OPERATOR_TOKEN_FILE=/etc/autoresearch/operator.token
unset AUTORESEARCH_OPERATOR_TOKEN
```

The command uses cryptographic randomness to generate 32 bytes (256 bits), encoded as 43 canonical, unpadded base64url characters. It creates a new mode-`0600` file containing the token and one trailing newline, prints the path but not the token, and never overwrites an existing path. Choose a new path during rotation rather than expecting the generator to replace a live credential.

For portable deployment, treat the Go token contract as authoritative. Token files must:

- be regular, non-symbolic-link files owned by the consuming process's effective UID;
- have exactly mode `0400` or `0600`, with no group or other permissions;
- contain the canonical token and at most the single trailing newline emitted by the generator; and
- be readable only by that UID and protected by appropriately private parent directories.

The UI parser accepts a broader printable high-entropy token representation, but the daemon and CLI enforce the canonical 43-character form. Always use `researchctl auth generate` rather than relying on that difference or hand-writing a password.

A token file cannot be shared by processes with different effective UIDs merely by changing its group. Either run `researchd`, the UI, and the CLI under the same UID, or create a separate owner-only regular file for each UID with the same generated token and configure each service's own path. Each copy must satisfy the ownership and mode checks. Compose automates this for its two UID-`65532` consumers; systemd and manual deployments must provision it explicitly. See `docs/deployment.md`.

### CLI use and offline commands

Once the file variable is exported, commands such as the following automatically send `Authorization: Bearer TOKEN`:

```bash
researchctl status
researchctl logs --follow
researchctl stop
researchctl run --config configs/autoresearch.yaml
```

Use `--api` to select a non-default API base URL. Remote API URLs must use HTTPS through the trusted TLS endpoint. The CLI refuses redirects instead of forwarding the token to another destination, so configure the final endpoint directly.

Only API-using commands load the credential. These local operations are credential-free:

- `researchctl init` for offline initialization;
- `researchctl validate --config PATH` for configuration validation; and
- `researchctl auth generate --file PATH` for credential provisioning.

`researchd` and UI startup are not offline operations and always require the credential. The daemon validates it before opening the listener or initializing runtime state, and then removes both operator variables from its own environment to avoid passing them to workloads.

## UI deployment and session settings

The UI reads the operator credential above plus the following environment variables at startup:

| Variable | Default | Rules |
| --- | --- | --- |
| `AUTORESEARCH_API` | `http://127.0.0.1:8080/api/v1` | Backend runtime API used by the UI server. The UI, not the browser, attaches the Bearer token. |
| `AUTORESEARCH_UI_HOST` | `127.0.0.1` | UI listen address. A non-loopback listen host requires an explicit Host allowlist. |
| `AUTORESEARCH_UI_PORT` | `8081` | TCP port, from 1 through 65535. |
| `AUTORESEARCH_UI_ALLOWED_HOSTS` | Derived for a loopback listener | Comma-separated exact lowercase-normalized `host` or `host:port` values. No wildcard, scheme, path, whitespace, userinfo, query, or fragment. |
| `AUTORESEARCH_UI_ALLOWED_ORIGINS` | `http://HOST:PORT` for loopback-only hosts | Comma-separated exact HTTP(S) origins, including scheme and non-default port. No credentials, path (including a trailing `/`), query, fragment, or whitespace. |
| `AUTORESEARCH_UI_SESSION_MAX_AGE` | `28800` | Finite session lifetime in seconds: greater than zero and at most `86400` (24 hours). The default is eight hours. |
| `NICEGUI_STORAGE_PATH` | `.nicegui` | Private server-side NiceGUI storage directory. It must be owned by the UI effective UID and is created or tightened to mode `0700`; the final path cannot be a symlink. |

For example, a loopback UI on `127.0.0.1:8081` defaults to Host `127.0.0.1:8081` and Origin `http://127.0.0.1:8081`. Compose additionally allows `localhost:8081` for its health check, but its allowed browser Origin remains `http://127.0.0.1:PORT`; use that exact URL for login rather than assuming `localhost` is interchangeable.

For a remote UI, set both allowlists explicitly, for example:

```bash
export AUTORESEARCH_UI_HOST=0.0.0.0
export AUTORESEARCH_UI_ALLOWED_HOSTS=research.example.test
export AUTORESEARCH_UI_ALLOWED_ORIGINS=https://research.example.test
export AUTORESEARCH_UI_SESSION_MAX_AGE=28800
export NICEGUI_STORAGE_PATH=/srv/autoresearch-private/nicegui
```

Every non-loopback Origin must use HTTPS. Remote deployment therefore requires an HTTPS reverse proxy or equivalent trusted TLS endpoint; setting a non-loopback listen address alone is not a secure deployment. The reverse proxy must preserve the exact allowed Host and apply its own network and request-rate controls. Include a container health checker's exact Host, such as `localhost:8081`, in the Host allowlist where applicable. An Origin allowlist entry is an origin, not a URL prefix.

Host and Origin checks apply to HTTP and WebSocket requests. Any supplied Origin must be allowed, while `POST`, `PUT`, `PATCH`, `DELETE`, and all WebSockets require an allowed Origin even if the browser omits it. This protects mutations, login/logout, and the NiceGUI socket against cross-site requests. Keep the allowed set minimal; broad wildcard or reflected-Origin support is intentionally absent.

Session cookies are `HttpOnly` and have a finite `max_age` and `SameSite=Strict`. `Secure` is enabled whenever any configured Origin uses HTTPS; use consistent HTTPS origins for remote deployments. The server-side session independently enforces an absolute expiry based on `AUTORESEARCH_UI_SESSION_MAX_AGE`, so activity does not extend authentication indefinitely. The UI generates a fresh independent 256-bit signing secret on every startup. Restarting the UI invalidates all existing session cookies; this is expected during rotation and also occurs during ordinary restarts.

Only `GET /health` is the public UI health endpoint. The login page/submission and narrowly required static/framework assets are public to make sign-in possible. Research pages, proxies, SSE, private framework resources, and WebSockets require an unexpired session. Login and authentication responses use `Cache-Control: no-store` where applicable.

## Files

`configs/autoresearch.yaml` composes agent definitions, one task, budgets, and executors. Model configuration is separate from agent configuration. Prompts are Markdown files outside the configuration tree. Examples with real providers are intentionally non-active and require operator-selected, currently supported model identifiers.

## Model contract

For a mixed OpenAI-Chief/Kimi-worker fleet, copy `configs/mixed-providers.example.yaml`, set the actual Astra model identifier and current prices, and export `OPENAI_API_KEY` plus `MOONSHOT_API_KEY`. Worker model instances are independent but share endpoint-level rate limits. The Kimi example uses the currently documented `kimi-k3.1-pro` identifier and `reasoning_effort: high`.

Fields include `provider`, `model`, `base_url`, `api_key_env`, `parameters`, `capabilities`, `retry`, `timeout`, `rate_limit`, `pricing`, `estimation`, and provider-specific options. Hosted real provider runs require per-million-token prices and `estimation.max_input_tokens`; prices are snapshotted and confirmed cost remains separate from reserved estimates. Secrets are environment-variable names or secret-store references, never inline values. `base_url_env` optionally overrides a model endpoint for container networks without putting credentials in YAML. Provider adapters declare accepted parameter names; an unsupported configured parameter is an error rather than silently ignored.

For native Kimi Code, use `provider: kimi-web` as shown in `configs/models/kimi-web.example.yaml`. `base_url_env` selects the local server, `token_file` identifies its owner-only bearer-token file, and `session_workspace` is the shared filesystem root under which the runtime creates a separate directory for each agent. `session_profile` is optional. Supported parameters are `reasoning_effort`, `permission_mode`, and `disabled_tools`; validate model identifiers and reasoning levels against the live server's `/api/v1/models`. Only `permission_mode: manual` is currently accepted for native built-in tools. The runtime's textual tool protocol is separate from those built-in tools and remains autonomous. A personal subscription is not per-token billing: do not configure `pricing`; use provider request quotas and endpoint rate limits instead. Monetary expenditure and token totals are unknown unless the provider reports them.

The native `KIMI_WEB_TOKEN_FILE` credential is distinct from `AUTORESEARCH_OPERATOR_TOKEN_FILE`. Native provider OAuth authorizes the Kimi transport and is not AutoResearch operator login; there is no operator OIDC/OAuth or RBAC integration yet.

`configs/kimi-web-demo.yaml` combines a scripted mock Chief with native workers for an explicitly initiated subscription-backed run. `configs/kimi-web-fleet.example.yaml` is the OpenAI-Chief/native-Kimi-worker template; its placeholder Chief model and real-provider prices must be configured before use. `make native-kimi-test` is the credential-free transport test and never submits prompts. “Credential-free” there means no paid/provider research prompt is required; its daemon still uses an isolated generated operator token under the current authentication contract.

Do not infer capabilities from a model name. Validate them against official provider documentation and explicit configuration. `max_output_tokens` maps to the adapter's supported request field. Reasoning effort and temperature are omitted unless both configured and supported.

## Agent contract

An agent definition selects a role, model reference, prompt, tool allowlist, context policy, permissions, and optional resource limits. Roles are behavior and tool policy, not separate infrastructure privileges. The Chief Scientist cannot grant itself a tool or change executor policy.

## Task contract

Tasks reference objective, completion criteria, restrictions, deliverables, and verification configuration. Paths resolve relative to the task file. Natural-language criteria guide the supervisor; machine-verifiable criteria gate objective completion.

## Budget contract

Monetary, token, request, runtime, parallelism, disk, CPU, and GPU dimensions are independent. Soft, hard, and finalization reserves must be non-negative and ordered. Zero-cost mock runs may use zero monetary limits while retaining request limits. Native subscription workers use request quotas, not fabricated token prices; `configs/kimi-web-budget.yaml` provides an illustrative bounded quota.

## Snapshots

A snapshot contains canonical YAML, SHA-256 hashes for every referenced file, the runtime version, and creation time. Secrets and environment values, including operator token values and token-file paths, are excluded. Restarts use the original snapshot rather than silently adopting edited configuration.

## Example

The active example under `configs/` is deterministic and free. Validation is offline and needs no operator token. Starting the daemon and running through the API do require one:

```bash
researchctl validate --config configs/autoresearch.yaml
researchctl auth generate --file /etc/autoresearch/operator.token
export AUTORESEARCH_OPERATOR_TOKEN_FILE=/etc/autoresearch/operator.token
unset AUTORESEARCH_OPERATOR_TOKEN
researchd --config configs/autoresearch.yaml --data-dir /srv/autoresearch-private
# In another shell with the same token-file setting:
researchctl run --config configs/autoresearch.yaml
```

The generation step is one-time provisioning and fails if the path already exists. Do not rerun it against the live path during an ordinary launch. See `docs/progress.md` for command availability and `docs/deployment.md` for complete service provisioning and rotation.
