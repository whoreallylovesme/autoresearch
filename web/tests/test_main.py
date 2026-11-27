from fastapi.testclient import TestClient

import autoresearch_ui.main as main
from autoresearch_ui.main import app


def test_health_endpoint():
    response = TestClient(app).get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_graph_routes_keep_raw_proxy_and_expose_normalized_elements(monkeypatch):
    snapshot = {
        "nodes": [{"id": "h1", "description": "", "type": "hypothesis", "status": "proposed"}],
        "edges": [{"id": "e1", "from_node": "h1", "to_node": "h1"}],
    }
    paths = []

    async def get(path):
        paths.append(path)
        return snapshot

    monkeypatch.setattr(main.runtime, "get", get)
    client = TestClient(app)

    raw_response = client.get("/runtime/graph")
    normalized_response = client.get("/internal/graph/elements")

    assert raw_response.status_code == 200
    assert raw_response.json() == snapshot
    assert normalized_response.status_code == 200
    assert normalized_response.json() == main.graph_elements(snapshot)
    assert paths == ["graph", "graph"]
