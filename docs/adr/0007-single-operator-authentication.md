# ADR 0007: Require single-operator authentication for every runtime interface

Status: Accepted

## Context

`researchd` exposes research state, logs, metrics, chat directives, and lifecycle controls. The NiceGUI control center and `researchctl` both use that API. Before this decision, possession of network reachability was effectively enough to operate the runtime.

Loopback-only binding is useful but is not an identity boundary. Another local UID, a compromised process, an accidentally published or forwarded port, or a container sharing network access can reach a loopback or private-listener endpoint. The API can start and stop work and issue authoritative directives, so an unauthenticated listener is unsafe even when it is not Internet-facing.

The project currently needs one operator, not a multi-tenant identity system. That operator may use a CLI, a browser, host services, or Compose services running under a fixed non-root UID. The design must therefore:

- fail closed when credentials are absent or unsafe, without blocking offline initialization, validation, and token generation;
- keep operator credentials out of agents, provider traffic, browsers, configuration snapshots, logs, source control, and image layers;
- protect browser mutations, WebSockets, and long-lived SSE connections, including after logout or expiry;
- support loopback by default and explicitly controlled HTTPS remote access;
- work across process UID boundaries without group/world-readable secrets; and
- provide understandable credential and session revocation.

Persistent research state and native provider authentication are separate concerns. Native Kimi OAuth and its server token authorize a provider transport; they are not an AutoResearch operator identity system.

## Decision

### One mandatory credential

Adopt a single machine-generated operator token for `researchd`, the UI backend, and all API-using `researchctl` commands. Require exactly one of `AUTORESEARCH_OPERATOR_TOKEN` or `AUTORESEARCH_OPERATOR_TOKEN_FILE`. Prefer the file form.

Generate 32 cryptographically random bytes with `researchctl auth generate --file PATH`, encoded as 43 canonical unpadded base64url characters. The generator creates a new mode-`0600` file with one trailing newline, prints only its path, and refuses to overwrite. Token files must be regular non-symlink files owned by the consuming process's effective UID and must use mode `0400` or `0600` for portable daemon/UI/CLI compatibility. Processes with different UIDs need separate UID-owned copies containing the same token; group sharing is not supported.

Keep `researchctl init`, `researchctl validate`, and `researchctl auth generate` credential-free because they do not call the authenticated API. Daemon and UI startup are never exempt. The daemon loads and validates the token before runtime initialization/listening and removes the credential environment variables before workloads can inherit them.

### Bearer API with a minimal liveness exception

Require exactly one Bearer Authorization header on every Go API request except exactly `GET /healthz`. Compare SHA-256 digests in constant time. Reject wrong, malformed, and duplicate credentials with a generic non-cacheable `401` before routing or request-body processing. Protect `/metrics`, SSE, and unknown paths as well as the versioned API. `/healthz/`, a health query string, and other methods do not inherit the exception.

Have `researchctl` load the same credential for REST and SSE and refuse redirects rather than forwarding the credential. The public health response exposes only liveness and does not stand in for readiness or research status.

### Server-side browser sessions

Do not send the operator Bearer token to browser storage or JavaScript. The UI verifies a login candidate using constant-time comparison of fixed-length digests, then creates an expiring server-side NiceGUI session. Authentication state contains only a boolean and absolute expiry, not the token.

Use finite `HttpOnly`, `SameSite=Strict` cookies, with `Secure` enabled for HTTPS-origin deployments. Default the session lifetime to eight hours and enforce a finite configured maximum of 24 hours. Store NiceGUI state in a UID-owned, non-symlink mode-`0700` `NICEGUI_STORAGE_PATH`.

Generate an independent random 256-bit cookie-signing secret at every UI startup. Do not derive it from the operator token or persist it as a reusable deployment secret. Every UI restart therefore revokes existing browser sessions.

Throttle failed logins by client address (five failures per 60 seconds, with bounded client history), bound login request bodies, and use generic non-cacheable failures. Keep this application throttle separate from infrastructure denial-of-service controls.

Permit unauthenticated UI access only to `GET /health`, the login GET/POST, favicon, and narrowly required static/framework assets. Protect every research page, runtime/internal proxy, SSE stream, private NiceGUI resource, and WebSocket.

### Request forgery and remote-access controls

Validate exact Host and Origin allowlists before public-route handling or session authentication. Reject any disallowed supplied Origin. Require an allowed Origin for unsafe HTTP methods and every WebSocket, including absent-Origin requests. Combined with Strict cookies, this is the CSRF and cross-site WebSocket defense.

Default both runtime interfaces to loopback. Require HTTPS for every non-loopback UI Origin and for remote API access. Require explicit exact allowlists for non-loopback UI deployment; do not support wildcards, path-bearing origins, or reflected origins. `--allow-non-loopback` changes only the daemon listener check and does not provide TLS or disable authentication.

Register browser SSE streams and NiceGUI clients by session. On logout or detected expiry, clear authentication, cancel streams and upstream responses, and disconnect clients. Recheck sessions in interactive callbacks and periodically during SSE so an already-open connection does not bypass revocation.

### Provisioning, build exclusion, and rotation

In Compose, mount the original file-backed secret only into a one-shot `operator-auth-init`. Have it copy the token into a dedicated private volume as UID `65532`, mode `0400`, then exit. Only `researchd` and `ui` mount that volume among long-running services, read-only; both run as UID `65532`. Keep NiceGUI storage on private UI tmpfs. Do not pass the operator token to model, native provider, or workload services.

For systemd/manual deployments, use one same-UID file or separate per-service copies. Unit files and environment examples contain paths, never real token values. Maintain Git and Docker context exclusions for environment files, token/secret/key patterns, private state, and `.nicegui`; runtime secret mounts must not become image build inputs.

Rotate by generating a replacement at a new path, rerunning the Compose initializer or replacing every host/service copy, and restarting both `researchd` and the UI. Daemon restart rejects the old Bearer credential. UI restart loads the new upstream token and creates a new signing secret, invalidating old UI sessions. Do not implement or document hot reload, multiple accepted token generations, or zero-downtime rotation in this version.

Defer OIDC, OAuth-based operator SSO, RBAC, per-user delegation, and read-only roles. They require a separate identity and authorization design and are not claimed by this ADR.

## Consequences

### Benefits

- Network reachability alone no longer grants research visibility or control, including on loopback.
- One credential contract covers CLI, daemon, UI backend, and browser login while browsers handle only revocable sessions.
- Authentication fails closed before routing/body work, with a deliberately tiny public liveness surface.
- Strict file ownership prevents unsafe secret sharing and makes cross-UID provisioning explicit.
- Host/Origin checks, Strict finite cookies, login throttling, callback checks, and SSE/client revocation address browser-specific threats rather than treating a Bearer check as sufficient.
- Compose limits token exposure to the initializer and its two non-root consumers. Repository/build exclusions reduce accidental credential persistence.
- Restart-based rotation has clear semantics: old API tokens and old browser sessions are both invalid after the required restarts.

### Costs and limitations

- Every online consumer needs provisioning; missing or invalid configuration stops startup or the CLI call.
- The token is a single full-privilege credential. There is no per-user attribution, role separation, or read-only access.
- Different service UIDs require synchronized copies, and rotation must update every copy.
- Both consumers must restart during rotation. There is a planned control-plane interruption and no token-overlap window.
- Every UI restart logs out all browsers, even without token rotation. Old persisted NiceGUI state cannot make an old cookie valid under the new signing secret.
- Public health probes reveal liveness. UI login and required static assets are also intentionally reachable, although Host/Origin checks still apply.
- Login throttling is per UI process and bounded; it is not a substitute for proxy/network rate limiting. Host root, service-UID compromise, token theft, and malicious TLS endpoints remain outside this control.
- Remote operation adds HTTPS proxy and exact Host/Origin configuration responsibility.

## Alternatives considered

### Optional authentication on loopback

Rejected. Loopback is a routing scope, not operator identity. Mandatory authentication removes a dangerous deployment-dependent behavior change and protects against local processes and accidental forwarding.

### Browser-held Bearer token or Basic authentication

Rejected. Putting the long-lived operator credential in browser storage, JavaScript, or every browser-to-daemon request increases exposure and makes logout/stream revocation weaker. A server-side session keeps the Bearer token in the UI backend and gives expiry and active-connection revocation a concrete enforcement point.

### Cookie-signing key derived from the operator token or persisted across restarts

Rejected. Signing and operator authentication are separate capabilities. An independent ephemeral startup secret ensures UI restart and rotation invalidate sessions and avoids another long-lived secret to provision and protect.

### Group-readable shared secret files

Rejected. Group access broadens exposure and does not establish a clear per-process ownership contract. Same-UID sharing or separate UID-owned copies is explicit and works with both Go and Python loaders.

### Hot rotation or multiple simultaneously accepted tokens

Deferred. A generation store, coordinated reload, and connection draining would add lifecycle complexity. The current single-token, restart-based mechanism is simpler to verify and ensures UI session invalidation.

### OAuth/OIDC and RBAC now

Deferred. The project has one operator and no role model or external identity requirement yet. No OAuth/OIDC operator login, per-user roles, or third-party identity dependency is part of this implementation. Native provider OAuth remains a distinct flow.

### Network perimeter alone

Rejected. TLS, firewalls, and reverse proxies remain necessary for remote deployments but do not replace application authentication, exact Host/Origin validation, or revocation.

## Verification requirements

Acceptance for this decision must demonstrate the following on the integrated implementation. These are requirements, not a claim that documentation work executed or completed acceptance:

1. **Offline/online startup matrix:** `researchctl init`, `validate`, and `auth generate` succeed without operator variables. Daemon, UI, and API CLI fail closed for missing, duplicate, malformed, and unsafe credentials without logging token material; the daemon opens no listener and creates no runtime state on credential failure.
2. **Generation and file safety:** generated tokens encode 32 random bytes in canonical 43-character base64url, repeated generation produces distinct values, output contains no token, existing paths are never overwritten, and mode/UID/symlink/non-regular-file rejection covers both Go and Python consumers. `0400` and `0600` same-UID files succeed.
3. **API boundary:** only exact `GET /healthz` bypasses Go authentication. Every registered route, `/metrics`, SSE, unknown routes, health variants, wrong/malformed/duplicate Authorization headers, and unauthorized request bodies follow the authenticated-before-routing contract. Valid Bearer credentials succeed, and CLI redirects never forward credentials.
4. **UI login and cookies:** correct login succeeds, incorrect login is generic and throttled, request bodies are bounded, sessions contain no token, absolute expiry is enforced, cookie lifetime is finite with HttpOnly and SameSite Strict, and HTTPS configuration enables Secure. A new UI startup signing secret invalidates an old cookie.
5. **Browser request controls:** exact Host/Origin acceptance and rejection cover public and protected routes, unsafe methods without Origin, cross-site WebSockets, private NiceGUI resources, and the intentionally public login/static subset. Non-loopback HTTP origins and wildcard/malformed allowlists fail validation.
6. **Revocation:** logout and expiry clear session state, stop registered SSE/upstream streams, disconnect NiceGUI clients, and prevent later callbacks or reconnects from acting. Long-lived streams reauthenticate after expiry rather than surviving indefinitely.
7. **Provisioning isolation:** rendered Compose configuration gives the source secret only to the initializer; only init/researchd/UI mount the destination volume, consumers mount it read-only, its file is UID `65532` mode `0400`, other services receive neither token variable, UI storage is private, and published host ports remain loopback by default. systemd examples have no inline token and document the required service UID/copy setup.
8. **Rotation:** after replacement, auth-init/copy updates, and both consumer restarts, the old API token and old UI session fail, the new token works for CLI and UI login, and the UI can call the daemon with the replacement. Restarting only one consumer is not accepted as complete rotation.
9. **Secret exclusions:** Git/Docker ignore and CI checks cover token/secret/environment/key and `.nicegui` patterns, and a clean build/deployment check finds no operator token in repository-tracked files, image layers, logs, snapshots, or unrelated service environments.
10. **Standard regression suites:** run the relevant integrated `make auth-test`, `make test`, `make test-integration`, `make web-test`, `make browser-test`, and deployment/Compose acceptance targets without relying on hosted/provider research prompts. Record any environment-specific unrun target explicitly rather than inferring success from unit tests alone.
