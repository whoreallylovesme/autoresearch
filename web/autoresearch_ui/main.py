from __future__ import annotations

import asyncio
import html
import json
import os
from pathlib import Path
from typing import Any

import httpx
from fastapi import Request
from fastapi.responses import JSONResponse, StreamingResponse
from nicegui import app, ui

from autoresearch_ui.api import RuntimeAPI
from autoresearch_ui.figures import budget_figure
from autoresearch_ui.views import format_money, graph_elements, hypothesis_counts, semantic_table

STATIC = Path(__file__).parent / "static"
API_BASE = os.getenv("AUTORESEARCH_API", "http://127.0.0.1:8080/api/v1").rstrip("/")
runtime = RuntimeAPI(API_BASE)
app.add_static_files("/static", str(STATIC))


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/runtime/graph")
async def runtime_graph() -> JSONResponse:
    return JSONResponse(await runtime.get("graph"))


@app.get("/internal/graph/elements")
async def internal_graph_elements() -> JSONResponse:
    return JSONResponse(graph_elements(await runtime.get("graph")))


@app.get("/runtime/events/stream")
async def runtime_events(request: Request) -> StreamingResponse:
    async def stream():
        client = httpx.AsyncClient(timeout=httpx.Timeout(None))
        headers = {}
        if request.headers.get("last-event-id"):
            headers["Last-Event-ID"] = request.headers["last-event-id"]
        try:
            async with client.stream(
                "GET", f"{API_BASE}/events/stream", params=request.query_params, headers=headers
            ) as response:
                async for chunk in response.aiter_raw():
                    if await request.is_disconnected():
                        break
                    yield chunk
        finally:
            await client.aclose()

    return StreamingResponse(stream(), media_type="text/event-stream", headers={"Cache-Control": "no-cache"})


def api_base() -> str:
    return API_BASE


async def shell(title: str) -> Any:
    ui.add_head_html('<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">')
    ui.add_head_html('<link rel="stylesheet" href="/static/theme.css">')
    ui.add_head_html('<script src="/static/live.js" defer></script>')
    ui.add_head_html('<script src="/static/cytoscape.min.js" defer></script>')
    ui.add_head_html('<script src="/static/graph.js" defer></script>')
    ui.add_head_html("<title>AutoResearch Control Center</title>")
    ui.link("Skip to research content", "#main-content").classes("skip-link")
    with ui.header().classes("bg-slate-950 text-white shadow-lg"):
        ui.label("AUTORESEARCH").classes("text-lg font-bold tracking-widest")
        with ui.row().classes("items-center gap-3"):
            for label, path in [
                ("Overview", "/"),
                ("Fleet", "/agents"),
                ("Graph", "/graph"),
                ("Budget", "/budget"),
                ("Experiments", "/experiments"),
                ("Events", "/events"),
                ("Report", "/report"),
                ("Playback", "/playback"),
            ]:
                ui.link(label, path).classes("text-slate-200")
    with ui.right_drawer().classes("chat-panel"):
        await chief_chat()
    main = (
        ui.element("main")
        .props('id="main-content" tabindex="-1"')
        .classes("w-full max-w-[1600px] mx-auto p-4 live-panel")
    )
    with main:
        ui.label(title).classes("text-3xl font-bold mb-4")
    ui.run_javascript("setTimeout(() => window.AutoResearchLive?.initializeVisibility(), 0)")
    return main


async def chief_chat() -> None:
    ui.label("Chief Scientist").classes("text-xl font-semibold")
    ui.label(
        "Persistent operator conversation. Directives are recorded separately from infrastructure policy."
    ).classes("muted text-sm")
    messages = ui.column().classes("w-full gap-2 max-h-[50vh] overflow-auto")
    directive = ui.checkbox("Authoritative operator directive", value=False)
    message = ui.textarea(label="Message to Chief Scientist").props('aria-label="Message to Chief Scientist" rows="3"')

    async def refresh() -> None:
        try:
            payload = await runtime.get("messages")
        except RuntimeError:
            return
        messages.clear()
        with messages:
            for item in (payload.get("messages") or [])[-50:]:
                body = item.get("payload") or {}
                text = body.get("text") or body.get("summary") or json.dumps(body)
                ui.label(f"{item.get('sender')} → {item.get('recipient')}: {text}").classes("text-sm")

    async def send() -> None:
        if not message.value.strip():
            ui.notify("Enter a message first", type="warning")
            return
        try:
            await runtime.chat(message.value, directive.value)
            message.value = ""
            await refresh()
        except (RuntimeError, ValueError) as error:
            ui.notify(str(error), type="negative")

    ui.button("Send message", on_click=send).props('aria-label="Send message to Chief Scientist"')
    await refresh()


async def control(action: str) -> None:
    try:
        await runtime.control(action)
        ui.notify(f"Runtime accepted: {action}", type="positive")
    except RuntimeError as error:
        ui.notify(str(error), type="negative")


@ui.page("/")
async def overview() -> None:
    main = await shell("Research control center")
    with main:
        state = ui.label()
        elapsed = ui.label().classes("muted")
        objective = ui.label().classes("text-lg max-w-5xl")
        with ui.row().classes("toolbar gap-2 my-4"):
            ui.button("Start", on_click=lambda: control("start"))
            ui.button("Pause", on_click=lambda: control("pause"))
            ui.button("Resume", on_click=lambda: control("resume"))
            ui.button("Finalize", on_click=lambda: control("finalize"))
            ui.button("Emergency stop", on_click=lambda: control("stop")).props('color="negative"')
        cards = ui.row().classes("metric-grid w-full")
        task_table = ui.column().classes("w-full mt-4")

        async def refresh() -> None:
            try:
                status, agents, tasks, graph, experiments, papers = await asyncio.gather(
                    runtime.get("run/status"),
                    runtime.get("agents"),
                    runtime.get("tasks"),
                    runtime.get("graph"),
                    runtime.get("experiments"),
                    runtime.get("papers"),
                )
            except RuntimeError as error:
                state.set_text(f"Runtime unavailable: {error}")
                return
            state.set_text(f"{status.get('name')} · {status.get('state')}")
            objective.set_text(status.get("objective", "Objective unavailable"))
            elapsed.set_text(f"Started {status.get('started_at', 'unavailable')} · API v1")
            agent_rows = agents.get("agents") or []
            task_rows = tasks.get("tasks") or []
            experiment_rows = experiments.get("experiments") or []
            paper_rows = papers.get("papers") or []
            hypotheses = hypothesis_counts(graph)
            metrics = [
                ("Agents active", sum(a.get("state") in {"running", "idle"} for a in agent_rows)),
                ("Failed agents", sum(a.get("state") == "failed" for a in agent_rows)),
                ("Pending tasks", sum(t.get("state") in {"pending", "retry_wait"} for t in task_rows)),
                ("Running experiments", sum(e.get("status") == "running" for e in experiment_rows)),
                ("Verified experiments", sum(e.get("status") == "verified" for e in experiment_rows)),
                ("Papers discovered", len(paper_rows)),
                ("Papers analyzed", sum(bool(p.get("pdf_artifact_id")) for p in paper_rows)),
                ("Hypotheses proposed", hypotheses["proposed"]),
                ("Hypotheses verified", hypotheses["verified"]),
                ("Hypotheses refuted", hypotheses["refuted"]),
                ("Confirmed spend", format_money(status.get("budget", {}).get("confirmed", {}).get("cost_micros"))),
                ("Reserved spend", format_money(status.get("budget", {}).get("reserved", {}).get("cost_micros"))),
            ]
            cards.clear()
            with cards:
                for label, value in metrics:
                    with ui.card().classes("metric-card"):
                        ui.label(str(value)).classes("metric-value")
                        ui.label(label).classes("metric-label")
            task_table.clear()
            with task_table:
                ui.html(
                    semantic_table(
                        "Current research tasks",
                        ["Task", "Role", "State", "Attempts", "Assigned agent"],
                        [
                            [
                                task.get("id"),
                                task.get("role"),
                                task.get("state"),
                                task.get("attempts"),
                                task.get("assigned_agent", "—"),
                            ]
                            for task in task_rows
                        ],
                    ),
                    sanitize=False,
                )

        await refresh()
        ui.timer(5.0, refresh)


@ui.page("/agents")
async def agents_page() -> None:
    main = await shell("Agent fleet")
    with main:
        table = ui.column().classes("w-full")
        details = ui.html().classes("code-block panel")

        async def refresh() -> None:
            try:
                payload = await runtime.get("agents")
            except RuntimeError as error:
                details.set_content(html.escape(str(error)))
                return
            rows = payload.get("agents") or []
            table.clear()
            with table:
                ui.html(
                    semantic_table(
                        "Configured agent fleet",
                        ["Agent", "Role", "Model", "Provider", "Task", "State", "Heartbeat", "Tokens"],
                        [
                            [
                                agent.get("id"),
                                agent.get("role"),
                                agent.get("model"),
                                agent.get("provider"),
                                agent.get("current_task", "—"),
                                agent.get("state"),
                                agent.get("last_heartbeat"),
                                agent.get("usage", {}).get("input_tokens", 0)
                                + agent.get("usage", {}).get("output_tokens", 0),
                            ]
                            for agent in rows
                        ],
                    ),
                    sanitize=False,
                )
            with table:
                for agent in rows:
                    ui.link(f"Open {agent.get('id')}", f"/agents/{agent.get('id')}").classes("text-sm")
            options = {agent.get("id"): agent.get("id") for agent in rows}
            details.set_content("<pre>" + html.escape(json.dumps(rows, indent=2)) + "</pre>") if not options else None

        await refresh()
        ui.timer(5.0, refresh)


@ui.page("/graph")
async def graph_page() -> None:
    main = await shell("Interactive research graph")
    with main:
        snapshot = await runtime.get("graph")
        with ui.row().classes("toolbar items-end gap-3"):
            ui.select(
                [
                    "",
                    "proposed",
                    "investigating",
                    "supported",
                    "verified",
                    "refuted",
                    "inconclusive",
                    "available",
                    "running",
                ],
                label="Status",
            ).props('id="graph-status-filter"')
            ui.input(label="Author filter").props('id="graph-author-filter"')
            ui.slider(min=0, max=100, value=100).props('id="graph-timeline" aria-label="Research timeline"')
            ui.button("Fit graph", on_click=lambda: ui.run_javascript("window.fitResearchGraph()"))
        with (
            ui.element("div")
            .classes("graph-region")
            .props('tabindex="0" role="region" aria-label="Pan and zoom research graph; accessible node table follows"')
        ):
            ui.element("div").props('id="research-graph"')
        ui.html(
            '<pre id="node-details" class="code-block panel">Select a node to inspect its evidence and revision.</pre>',
            sanitize=False,
        )
        with ui.expansion("Accessible node table", value=True).classes("w-full"):
            ui.html(
                semantic_table(
                    "Research graph nodes",
                    ["Node", "Type", "Status", "Author", "Revision", "Description"],
                    [
                        [
                            node.get("id"),
                            node.get("type"),
                            node.get("status"),
                            node.get("author"),
                            node.get("revision"),
                            node.get("description"),
                        ]
                        for node in snapshot.get("nodes") or []
                    ],
                ),
                sanitize=False,
            )
        ui.run_javascript(f"window.initResearchGraph({json.dumps(graph_elements(snapshot))});")


@ui.page("/budget")
async def budget_page() -> None:
    main = await shell("Budget and provider accounting")
    with main:
        metrics = ui.label().classes("text-lg")
        chart = ui.plotly(budget_figure({})).classes("w-full h-[420px]")
        ui.label(
            "Spending over time: Unavailable — the current API exposes snapshots, not a confirmed time series."
        ).classes("muted")
        token_table = ui.column().classes("w-full")

        async def refresh() -> None:
            try:
                status, agents = await asyncio.gather(runtime.get("budget"), runtime.get("agents"))
            except RuntimeError as error:
                metrics.set_text(str(error))
                return
            confirmed = format_money(status.get("confirmed", {}).get("cost_micros"))
            reserved = format_money(status.get("reserved", {}).get("cost_micros"))
            uncertain = format_money(status.get("uncertain", {}).get("cost_micros"))
            remaining = format_money(status.get("estimated_remaining_cost_micros"))
            reserve = format_money(status.get("finalization_reserve_cost_micros"))
            hard_limit = format_money(status.get("hard_limit_cost_micros"))
            metrics.set_text(
                f"Hard limit {hard_limit} · Confirmed {confirmed} · Reserved {reserved} · "
                f"Uncertain {uncertain} · Remaining estimate {remaining} · Finalization reserve {reserve}"
            )
            chart.options = budget_figure(status).to_plotly_json()
            chart.update()
            token_table.clear()
            with token_table:
                ui.html(
                    semantic_table(
                        "Token usage by agent and model",
                        ["Agent", "Model", "Input", "Output", "Reasoning", "Cached"],
                        [
                            [
                                a.get("id"),
                                a.get("model"),
                                a.get("usage", {}).get("input_tokens"),
                                a.get("usage", {}).get("output_tokens"),
                                a.get("usage", {}).get("reasoning_tokens"),
                                a.get("usage", {}).get("cached_tokens"),
                            ]
                            for a in agents.get("agents") or []
                        ],
                    ),
                    sanitize=False,
                )

        await refresh()
        ui.timer(5.0, refresh)


@ui.page("/experiments")
async def experiments_page() -> None:
    main = await shell("Experiments and verification")
    with main:
        table = ui.column().classes("w-full")
        detail = ui.html().classes("code-block panel")

        async def refresh() -> None:
            try:
                payload = await runtime.get("experiments")
            except RuntimeError as error:
                detail.set_content(html.escape(str(error)))
                return
            records = payload.get("experiments") or []
            table.clear()
            with table:
                ui.html(
                    semantic_table(
                        "Reproducible experiment records",
                        ["Experiment", "Status", "Executor", "Metrics", "Verification", "Artifacts"],
                        [
                            [
                                r.get("id"),
                                r.get("status"),
                                r.get("executor_name"),
                                json.dumps(r.get("metrics", {})),
                                (r.get("verification") or {}).get("passed", "Unavailable"),
                                len(r.get("artifacts") or []),
                            ]
                            for r in records
                        ],
                    ),
                    sanitize=False,
                )
            detail.set_content("<pre>" + html.escape(json.dumps(records, indent=2)) + "</pre>")

        await refresh()
        ui.timer(5.0, refresh)


@ui.page("/events")
async def events_page() -> None:
    main = await shell("Live research events")
    with main:
        payload = await runtime.get("events")
        actors = sorted({event.get("actor") for event in payload.get("events") or []})
        types = sorted({event.get("type") for event in payload.get("events") or []})
        with ui.row().classes("toolbar gap-3"):
            ui.select([""] + actors, label="Agent").props('id="event-agent-filter" onchange="AutoResearchLive.reset()"')
            ui.select([""] + types, label="Event type").props(
                'id="event-type-filter" onchange="AutoResearchLive.reset()"'
            )
        ui.label("Live updates: connecting").props('id="live-status"').classes("muted")
        ui.element("ol").props('id="event-list" aria-live="polite"').classes("event-list")


@ui.page("/report")
async def report_page() -> None:
    main = await shell("Research report")
    with main:
        content = ui.html().classes("report-content panel")

        async def refresh() -> None:
            try:
                payload = await runtime.get("report")
            except RuntimeError as error:
                content.set_content(html.escape(str(error)))
                return
            report = payload.get("report") or "No report is available yet."
            content.set_content("<pre>" + html.escape(report) + "</pre>")

        await refresh()
        ui.timer(5.0, refresh)


@ui.page("/agents/{agent_id}")
async def agent_detail_page(agent_id: str) -> None:
    main = await shell(f"Agent {agent_id}")
    with main:
        agents_payload, tasks_payload, messages_payload, events_payload = await asyncio.gather(
            runtime.get("agents"), runtime.get("tasks"), runtime.get("messages"), runtime.get("events")
        )
        agent = next((item for item in agents_payload.get("agents") or [] if item.get("id") == agent_id), None)
        if agent is None:
            ui.label("Agent not found").classes("text-xl text-red-300")
            return
        ui.html("<pre>" + html.escape(json.dumps(agent, indent=2)) + "</pre>", sanitize=False).classes(
            "code-block panel"
        )
        ui.html(
            semantic_table(
                "Agent task history",
                ["Task", "State", "Attempts", "Last error"],
                [
                    [task.get("id"), task.get("state"), task.get("attempts"), task.get("last_error", "")]
                    for task in tasks_payload.get("tasks") or []
                    if task.get("assigned_agent") == agent_id
                ],
            ),
            sanitize=False,
        )
        ui.html(
            semantic_table(
                "Agent messages and tool activity",
                ["Time", "Type", "Sender", "Task"],
                [
                    [event.get("timestamp"), event.get("type"), event.get("actor"), event.get("task_id", "")]
                    for event in events_payload.get("events") or []
                    if event.get("actor") == agent_id
                ],
            ),
            sanitize=False,
        )


@ui.page("/playback")
async def playback_page() -> None:
    main = await shell("Research playback")
    with main:
        events_payload = await runtime.get("events")
        maximum = max([event.get("sequence", 0) for event in events_payload.get("events") or []] or [0])
        playback = {"until": maximum, "playing": False, "speed": 1}
        status = ui.label().classes("text-lg")
        event_label = ui.label().classes("muted")
        tasks_table = ui.column().classes("w-full")
        graph_table = ui.column().classes("w-full")
        experiments_table = ui.column().classes("w-full")
        report = ui.html().classes("report-content panel")

        async def refresh() -> None:
            frame = await runtime.get(f"playback?until={playback['until']}")
            status.set_text(f"Event {frame.get('until')} / {maximum} · {frame.get('research_state')}")
            history = frame.get("events") or []
            if history:
                latest = history[-1]
                event_label.set_text(f"{latest.get('timestamp')} · {latest.get('type')} · {latest.get('actor')}")
            tasks_table.clear()
            with tasks_table:
                ui.html(
                    semantic_table(
                        "Tasks at selected event",
                        ["Task", "State", "Agent", "Attempts"],
                        [
                            [t.get("id"), t.get("state"), t.get("assigned_agent", "—"), t.get("attempts")]
                            for t in frame.get("tasks") or []
                        ],
                    ),
                    sanitize=False,
                )
            hypotheses = [
                node for node in (frame.get("graph", {}).get("nodes") or []) if node.get("type") == "hypothesis"
            ]
            graph_table.clear()
            with graph_table:
                ui.html(
                    semantic_table(
                        "Hypotheses at selected event",
                        ["Hypothesis", "Status", "Revision"],
                        [[h.get("id"), h.get("status"), h.get("revision")] for h in hypotheses],
                    ),
                    sanitize=False,
                )
            experiments_table.clear()
            with experiments_table:
                ui.html(
                    semantic_table(
                        "Experiments at selected event",
                        ["Experiment", "Status", "Metrics"],
                        [
                            [e.get("id"), e.get("status"), json.dumps(e.get("metrics", {}))]
                            for e in frame.get("experiments") or []
                        ],
                    ),
                    sanitize=False,
                )
            report.set_content(
                "<pre>" + html.escape(frame.get("report") or "Report is not finalized at this event.") + "</pre>"
            )

        def seek(value: float) -> None:
            playback["until"] = int(value)
            playback["playing"] = False
            play_button.set_text("Play")

        async def tick() -> None:
            if not playback["playing"]:
                return
            playback["until"] = min(maximum, playback["until"] + playback["speed"])
            if playback["until"] >= maximum:
                playback["playing"] = False
                play_button.set_text("Play")
            slider.set_value(playback["until"])
            await refresh()

        def toggle_play() -> None:
            playback["playing"] = not playback["playing"]
            play_button.set_text("Pause" if playback["playing"] else "Play")

        with ui.row().classes("toolbar items-center gap-3"):
            play_button = ui.button("Play", on_click=toggle_play)
            speed = ui.select([1, 2, 4], value=1, label="Events per tick")
            speed.on_value_change(lambda event: playback.update(speed=int(event.value)))
            slider = (
                ui.slider(min=0, max=maximum, value=maximum, on_change=lambda event: seek(event.value))
                .classes("w-2/3")
                .props('aria-label="Research event timeline"')
            )
        ui.timer(1.0, tick)
        await refresh()


if __name__ == "__main__":
    ui.run(
        host=os.getenv("AUTORESEARCH_UI_HOST", "127.0.0.1"),
        port=int(os.getenv("AUTORESEARCH_UI_PORT", "8081")),
        title="AutoResearch Control Center",
        show=False,
        reload=False,
    )
