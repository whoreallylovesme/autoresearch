from __future__ import annotations

from typing import Any

import plotly.graph_objects as go


def budget_figure(status: dict[str, Any]) -> go.Figure:
    providers = status.get("providers") or {}
    if providers:
        names = sorted(providers)
        accounts = [providers[name] for name in names]
    else:
        names = ["Total"]
        accounts = [status]
    series = [("Confirmed", "#63e6be"), ("Reserved", "#f8c471"), ("Uncertain", "#ff9f9f")]
    figure = go.Figure()
    for name, color in series:
        key = name.lower()
        values = [(account.get(key, {}).get("cost_micros", 0) / 1_000_000) for account in accounts]
        figure.add_bar(name=name, x=names, y=values, marker_color=color)
    figure.update_layout(
        barmode="stack",
        title="Provider cost accounting (USD)",
        paper_bgcolor="#111827",
        plot_bgcolor="#111827",
        font_color="#e5e7eb",
        yaxis_title="USD",
        legend_orientation="h",
        margin={"l": 60, "r": 20, "t": 60, "b": 40},
    )
    return figure
