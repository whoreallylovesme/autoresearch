# Web Interface

**Implementation status:** The NiceGUI control center is implemented with overview/fleet/agent-detail, Cytoscape.js graph with accessible node table, Plotly budget, experiments, resumable live SSE events, report view, persistent Chief chat, and start/pause/resume/finalize/emergency-stop controls. A same-origin proxy avoids direct browser-to-daemon access. Browser acceptance runs in headless Chromium on the VPS and CI.

The Python control center is a first-class operator interface using the same versioned API as `researchctl`. NiceGUI is the primary application framework, Plotly provides analytical charts, and Cytoscape.js provides the interactive graph. No custom TypeScript application is planned.

## Design direction

The public interface at `https://pequalnp.com/` was inspected for interaction ideas: a prominent objective and verdict, live fleet counts, recent agent messages, research-direction status, evidence-linked records, and explicit refuted/dead directions. AutoResearch adopts these information-architecture ideas without copying branding, layout assets, text, or proprietary code. Its visual language is a restrained technical control center with accessible status colors and density controls.

## Views

- **Overview:** objective, lifecycle, elapsed time, confirmed and reserved budget, tokens, quotas, agents, tasks, experiments, papers, and hypothesis counts.
- **Agent fleet:** role, model, provider, task, state, heartbeat, meaningful activity, usage, cost, errors, retries, logs, messages, tools, and artifacts. Detail pages show available outputs, never invented private reasoning traces.
- **Research graph:** pan/zoom, node details, status/author/direction filters, evidence links, dependencies, timeline, and live revisions.
- **Budget:** total, confirmed, reserved, finalization reserve, estimated remaining, provider/agent/direction breakdowns, time series, tokens, and quotas. Unknown metrics display `Unavailable`.
- **Experiments:** status, executor, configuration, revision, runtime, hardware metrics when available, logs, results, verification, and artifacts.
- **Events:** chronological feed with agent, task, type, severity, and direction filters and resumable live updates.
- **Chief chat:** persistent operator conversation and distinct authoritative directive controls alongside the current view.
- **Playback:** implemented event-sequence replay with play, pause, seek, speed controls, and task/hypothesis/experiment/conclusion projections.

## Real-time contract

The browser loads an API snapshot, then consumes SSE by per-run sequence. Reconnect uses `Last-Event-ID`; a sequence gap triggers snapshot reconciliation. Controls are REST operations and return durable operation IDs. The UI does not infer success before the runtime commits it.

## Security

Loopback-only is the default. Remote deployment requires TLS, operator authentication, secure cookies, CSRF protection for browser mutations, and no provider credentials in browser responses. Logs and external content are escaped as untrusted text.
