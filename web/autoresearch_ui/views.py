from __future__ import annotations

import html
from collections.abc import Iterable
from typing import Any


def semantic_table(caption: str, headers: list[str], rows: Iterable[Iterable[Any]]) -> str:
    header_markup = "".join(f'<th scope="col">{html.escape(header)}</th>' for header in headers)
    body_markup = []
    for row in rows:
        cells = []
        for index, value in enumerate(row):
            tag = "th" if index == 0 else "td"
            scope = ' scope="row"' if index == 0 else ""
            cells.append(f"<{tag}{scope}>{html.escape(str(value))}</{tag}>")
        body_markup.append("<tr>" + "".join(cells) + "</tr>")
    labels = ", ".join(
        f'--label-{index}: "{html.escape(header, quote=True)}"' for index, header in enumerate(headers, 1)
    )
    return (
        '<div class="table-wrapper" tabindex="0" role="region" '
        f'aria-label="{html.escape(caption, quote=True)}">'
        f'<table style="{labels}"><caption>{html.escape(caption)}</caption>'
        f"<thead><tr>{header_markup}</tr></thead><tbody>{''.join(body_markup)}</tbody></table></div>"
    )


def graph_elements(snapshot: dict[str, Any]) -> list[dict[str, Any]]:
    elements = []
    for node in snapshot.get("nodes") or []:
        elements.append(
            {
                "group": "nodes",
                "data": {
                    "id": node["id"],
                    "label": node.get("description", node["id"])[:42],
                    "type": node.get("type", "unknown"),
                    "status": node.get("status", "unknown"),
                    "author": node.get("author", "unknown"),
                    "created_at": node.get("created_at", ""),
                    "revision": node.get("revision", 1),
                    "description": node.get("description", ""),
                },
                "classes": f"{node.get('type', 'unknown')} status-{node.get('status', 'unknown')}",
            }
        )
    for edge in snapshot.get("edges") or []:
        elements.append(
            {
                "group": "edges",
                "data": {
                    "id": edge["id"],
                    "source": edge["from_node"],
                    "target": edge["to_node"],
                    "label": edge.get("type", "related"),
                },
            }
        )
    return elements


def hypothesis_counts(snapshot: dict[str, Any]) -> dict[str, int]:
    counts = {"proposed": 0, "verified": 0, "refuted": 0}
    for node in snapshot.get("nodes") or []:
        if node.get("type") == "hypothesis" and node.get("status") in counts:
            counts[node["status"]] += 1
    return counts


def format_money(micros: int | float | None) -> str:
    if micros is None:
        return "Unavailable"
    return f"${micros / 1_000_000:.6f}"
