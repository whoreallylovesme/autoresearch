from autoresearch_ui.figures import budget_figure
from autoresearch_ui.views import graph_elements, hypothesis_counts, semantic_table


def test_semantic_table_escapes_content_and_preserves_structure():
    rows = [["<script>alert(1)</script>", "running"]]
    markup = semantic_table("Agent fleet", ["Agent", "State"], rows)
    assert "<table " in markup
    assert "<caption" in markup
    assert 'scope="col"' in markup
    assert "&lt;script&gt;" in markup
    assert "<script>alert(1)</script>" not in markup
    assert 'tabindex="0"' in markup
    assert 'role="region"' in markup
    assert "--label-1" in markup


def test_graph_elements_and_hypothesis_counts():
    snapshot = {
        "nodes": [
            {"id": "h1", "type": "hypothesis", "status": "verified", "description": "A < B", "author": "chief"},
            {"id": "p1", "type": "paper", "status": "available", "description": "Paper", "author": "fetcher"},
            {"id": "h2", "type": "hypothesis", "status": "refuted", "description": "Bad", "author": "chief"},
        ],
        "edges": [{"id": "e1", "from_node": "h1", "to_node": "p1", "type": "cites"}],
    }
    elements = graph_elements(snapshot)
    assert len(elements) == 4
    assert elements[0]["data"]["status"] == "verified"
    assert elements[-1]["data"]["source"] == "h1"
    assert hypothesis_counts(snapshot) == {"proposed": 0, "verified": 1, "refuted": 1}


def test_graph_elements_normalizes_descriptions_and_edge_defaults():
    snapshot = {
        "nodes": [
            {"id": "missing"},
            {"id": "empty", "description": ""},
        ],
        "edges": [{"id": "e1", "from_node": "missing", "to_node": "empty"}],
    }

    assert graph_elements(snapshot) == [
        {
            "group": "nodes",
            "data": {
                "id": "missing",
                "label": "missing",
                "type": "unknown",
                "status": "unknown",
                "author": "unknown",
                "created_at": "",
                "revision": 1,
                "description": "",
            },
            "classes": "unknown status-unknown",
        },
        {
            "group": "nodes",
            "data": {
                "id": "empty",
                "label": "empty",
                "type": "unknown",
                "status": "unknown",
                "author": "unknown",
                "created_at": "",
                "revision": 1,
                "description": "",
            },
            "classes": "unknown status-unknown",
        },
        {
            "group": "edges",
            "data": {"id": "e1", "source": "missing", "target": "empty", "label": "related"},
        },
    ]


def test_budget_figure_uses_confirmed_reserved_and_uncertain():
    status = {
        "confirmed": {"cost_micros": 40},
        "reserved": {"cost_micros": 20},
        "uncertain": {"cost_micros": 10},
        "providers": {
            "openai": {
                "confirmed": {"cost_micros": 40},
                "reserved": {"cost_micros": 20},
                "uncertain": {"cost_micros": 10},
            }
        },
    }
    figure = budget_figure(status)
    assert figure.data[0].name == "Confirmed"
    assert list(figure.data[0].y) == [0.00004]
    assert list(figure.data[1].y) == [0.00002]
    assert list(figure.data[2].y) == [0.00001]
