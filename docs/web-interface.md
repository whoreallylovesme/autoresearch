# Web Interface

**Implementation status:** The NiceGUI control center is implemented with single-operator login/logout, expiring server-side sessions, overview/fleet/agent-detail, Cytoscape.js graph with accessible node table, Plotly budget, experiments, resumable live SSE events, report view, persistent Chief chat, and start/pause/resume/finalize/emergency-stop controls. A same-origin authenticated proxy avoids direct browser-to-daemon access. Browser acceptance coverage runs in headless Chromium on the VPS and CI; see `docs/adr/0007-single-operator-authentication.md` for the authentication verification requirements.

The Python control center is a first-class operator interface using the same versioned API as `researchctl`. NiceGUI is the primary application framework, Plotly provides analytical charts, and Cytoscape.js provides the interactive graph. No custom TypeScript application is planned.

## Design direction

The public interface at `https://pequalnp.com/` was inspected for interaction ideas: a prominent objective and verdict, live fleet counts, recent agent messages, research-direction status, evidence-linked records, and explicit refuted/dead directions. AutoResearch adopts these information-architecture ideas without copying branding, layout assets, text, or proprietary code. Its visual language is a restrained technical control center with accessible status colors and density controls.

## Operator sign-in

The UI and `researchctl` use the same generated operator token. Provision exactly one of `AUTORESEARCH_OPERATOR_TOKEN` or `AUTORESEARCH_OPERATOR_TOKEN_FILE` before UI startup; the file form is preferred. The UI loads the token in the Python server, uses it to authenticate upstream daemon requests, and never places it in a browser response, NiceGUI session, or JavaScript. The daemon scrubs its own operator environment after loading so workloads do not inherit it.

Open the exact configured UI URL, normally `http://127.0.0.1:8081`, and enter the token on the password-style login form. The optional show-password control explicitly warns that it displays the token. Login responses use `Cache-Control: no-store`, and failures have a generic error rather than revealing whether a candidate was close to the configured value. Candidate verification hashes both values and uses constant-time comparison of fixed-length SHA-256 digests.

Login is throttled to five failed attempts per client address in a 60-second window, with bounded history for at most 1024 client keys and login bodies capped at 8 KiB. Successful login clears that client's failure history. This is an application-level brute-force control, not a replacement for reverse-proxy rate limits or denial-of-service protection. Deployments behind a proxy must also ensure that client-address handling is trustworthy and that forwarded headers are sanitized by the proxy.

There is no OAuth/OIDC login, RBAC, per-user role, or read-only session yet. Possession of the token grants the single full operator identity. Native Kimi provider OAuth is a separate transport-provisioning flow and does not sign an operator into AutoResearch.

## Server-side sessions and cookies

Successful login establishes a server-side NiceGUI session whose authentication fields are `authenticated` and an absolute `expires_at`. The token itself is not stored in those fields. A signed session cookie identifies the browser session; it has:

- a finite `max_age` controlled by `AUTORESEARCH_UI_SESSION_MAX_AGE`;
- `HttpOnly`;
- `SameSite=Strict`; and
- `Secure` whenever any configured allowed Origin uses HTTPS.

The default lifetime is 28800 seconds (eight hours), and values must be greater than zero and no more than 86400 seconds (24 hours). Server-side expiry is absolute and independently checked, so callbacks and streams cannot remain authenticated indefinitely merely because a browser retains a cookie. Expired or malformed authentication state is cleared and requires a new login.

The session-cookie signing secret is generated from an independent random 32-byte value at every UI startup. It is not the operator token, is not derived from it, and is not configured through provider OAuth. Restarting the UI therefore invalidates all old cookies and browser sessions even when token rotation is not intended. Token rotation always restarts the UI and has the same session-invalidation effect.

NiceGUI's server-side storage uses `NICEGUI_STORAGE_PATH` (default `.nicegui`). The final path must be a real directory owned by the UI effective UID, not a symlink, and is created or tightened to mode `0700`. Compose places it on a private UID-`65532` tmpfs; the systemd template combines `/tmp/nicegui` with `PrivateTmp=true`. Do not share this storage through a public or group-writable directory, and keep it out of source and Docker build contexts.

Log out with the header's **Log out** button. Logout is an Origin-protected `POST /logout`, clears server-side authentication state, cancels the session's active SSE streams, and disconnects its registered NiceGUI clients. Closing a tab alone is not an explicit logout.

## Host, Origin, CSRF, and WebSocket controls

Loopback-only remains the default. Remote access requires HTTPS and explicit exact allowlists:

- `AUTORESEARCH_UI_ALLOWED_HOSTS` contains exact `host` or `host:port` entries, with no wildcard or URL syntax.
- `AUTORESEARCH_UI_ALLOWED_ORIGINS` contains exact `http`/`https` origins with no path, query, fragment, or credentials. Every non-loopback origin must use HTTPS.

See `docs/configuration.md` for defaults and validation and `docs/deployment.md` for a reverse-proxy example. A reverse proxy must preserve the allowed Host rather than rewriting it to an unrelated internal name. Container health-check Host entries, such as `localhost:8081`, must remain in the Host allowlist when the supplied Compose health check is used.

The middleware enforces Host and Origin before deciding whether a route is public. An unknown Host receives `400`; a disallowed Origin receives `403`. Any supplied Origin must match the allowlist. `POST`, `PUT`, `PATCH`, `DELETE`, and all WebSockets require an allowed Origin even when it is absent. This explicit Origin validation, together with the Strict cookie, is the CSRF and cross-site WebSocket control; it also protects login and logout. Do not add wildcard/reflected origins or move tokens into URL query parameters.

WebSockets and NiceGUI polling/private resources use the same server-side session authentication as pages. An unauthenticated or expired WebSocket is rejected with a policy close rather than being accepted and trusted based on an earlier page load. Interactive client callbacks recheck session validity before performing refreshes, controls, or chat operations.

Only the `GET /health` path is the public UI health endpoint. The UI also exposes `GET`/`POST /login`, `GET /favicon.ico`, `/static/` assets, and narrowly matched versioned NiceGUI static/library/component/ESM assets because the login page and framework must load before authentication. No research, runtime, internal, SSE, dynamic/private NiceGUI resource, or application page is public. Unauthenticated HTML page requests redirect to `/login` with `303`; runtime/internal/SSE/framework API-style requests receive JSON `401` with `Cache-Control: no-store`.

## Views

- **Overview:** objective, lifecycle, elapsed time, confirmed and reserved budget, tokens, quotas, agents, tasks, experiments, papers, and hypothesis counts.
- **Agent fleet:** role, model, provider, task, state, heartbeat, meaningful activity, usage, cost, errors, retries, logs, messages, tools, and artifacts. Detail pages show available outputs, never invented private reasoning traces.
- **Research graph:** pan/zoom, node details, status/author/direction filters, evidence links, dependencies, timeline, and live revisions.
- **Budget:** total, confirmed, reserved, finalization reserve, estimated remaining, provider/agent/direction breakdowns, time series, tokens, and quotas. Unknown metrics display `Unavailable`.
- **Experiments:** status, executor, configuration, revision, runtime, hardware metrics when available, logs, results, verification, and artifacts.
- **Events:** chronological feed with agent, task, type, severity, and direction filters and resumable live updates.
- **Chief chat:** persistent operator conversation and distinct authoritative directive controls alongside the current view.
- **Playback:** implemented event-sequence replay with play, pause, seek, speed controls, and task/hypothesis/experiment/conclusion projections.

All research views and controls require a valid UI session. The single operator identity does not grant models or agents access to the UI token, and displayed logs or external content remain escaped as untrusted text.

## Real-time contract and revocation

The browser loads an authenticated UI snapshot, then consumes same-origin SSE by per-run sequence. Reconnect uses `Last-Event-ID`; a sequence gap triggers snapshot reconciliation. Controls are REST operations and return durable operation IDs. The UI does not infer success before the runtime commits it.

For `/runtime/events/stream`, the Python backend opens the daemon's authenticated `/api/v1/events/stream` using the server-held Bearer token. It validates event cursors, strips browser Cookie headers from the upstream request, and proxies only the event stream. Browser clients never need direct network access to `researchd` and never receive its Bearer credential.

SSE authorization is not just a connection-time check. Streams are registered by session ID, revalidate the active session at roughly one-second intervals, and listen for explicit cancellation. Logout or detected expiry closes those streams and their upstream responses. The same revocation disconnects registered NiceGUI/Socket.IO clients, while subsequent callbacks and reconnects require a fresh valid session. UI process restart also tears down the connections and, through the new signing secret, prevents old cookies from establishing replacements.

## Operational security

Use the same UID for the UI and daemon token file or provision separate UID-owned copies containing the same token. Compose performs the copy into its private UID-`65532`, mode-`0400` volume; host/systemd deployments must do it explicitly. Keep provider credentials and operator credentials out of browser responses, logs, images, snapshots, and generated artifacts.

Rotation replaces the token, reruns Compose `operator-auth-init` or updates every host copy, and restarts both `researchd` and the UI. The daemon then rejects the old API token, and the restarted UI rejects old sessions and uses the new token upstream. See `docs/deployment.md` for commands and `docs/security.md` for threat boundaries.
