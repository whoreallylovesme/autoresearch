import asyncio
import json

import httpx
import pytest

from autoresearch_ui.api import RuntimeAPI

TEST_TOKEN = "unit-test-operator-token-7f3a9c5e1b8d4f6a2c0e"


def test_runtime_api_uses_versioned_endpoints_and_chat():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/api/v1/chat":
            return httpx.Response(200, json={"response": "Current state"})
        return httpx.Response(200, json={"state": "running", "objective": "Research"})

    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        api = RuntimeAPI("http://runtime/api/v1", client=client, token=TEST_TOKEN)
        status = await api.get("run/status")
        response = await api.chat("Explain", directive=False)
        await client.aclose()
        assert status["objective"] == "Research"
        assert response["response"] == "Current state"

    asyncio.run(scenario())
    assert all(request.headers["Authorization"] == f"Bearer {TEST_TOKEN}" for request in requests)
    assert requests[0].url.path == "/api/v1/run/status"
    assert requests[1].url.path == "/api/v1/chat"
    assert json.loads(requests[1].content) == {"message": "Explain", "directive": False}


def test_runtime_api_surfaces_safe_error():
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(503, text="provider unavailable")

    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        api = RuntimeAPI("http://runtime/api/v1", client=client, token=TEST_TOKEN)
        try:
            await api.get("run/status")
        except RuntimeError as error:
            assert "503" in str(error)
            assert "provider unavailable" in str(error)
        else:
            raise AssertionError("expected RuntimeError")
        await client.aclose()

    asyncio.run(scenario())


def test_runtime_api_requires_a_valid_operator_token():
    with pytest.raises(ValueError):
        RuntimeAPI("http://runtime/api/v1", token="short")


def test_raw_event_stream_attaches_only_backend_auth_and_validated_cursors():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        return httpx.Response(
            200,
            headers={"Content-Type": "text/event-stream"},
            content=b"id: 43\nevent: research.started\ndata: {}\n\n",
        )

    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        api = RuntimeAPI("http://runtime/api/v1", client=client, token=TEST_TOKEN)
        response = await api.open_event_stream(last_event_id="42", after="41")
        assert response.status_code == 200
        assert await response.aread() == b"id: 43\nevent: research.started\ndata: {}\n\n"
        await response.aclose()
        await client.aclose()

    asyncio.run(scenario())
    request = requests[0]
    assert request.headers["Authorization"] == f"Bearer {TEST_TOKEN}"
    assert request.headers["Last-Event-ID"] == "42"
    assert request.headers.get("Cookie") is None
    assert dict(request.url.params) == {"after": "41"}
    assert "browser-secret" not in str(request.headers)


@pytest.mark.parametrize("cursor", ["", "-1", "1x", " 1", "9223372036854775808", 1.5, True])
def test_raw_event_stream_rejects_invalid_cursors(cursor):
    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(lambda _: (_ for _ in ()).throw(AssertionError())))
        api = RuntimeAPI("http://runtime/api/v1", client=client, token=TEST_TOKEN)
        with pytest.raises(ValueError):
            await api.open_event_stream(last_event_id=cursor)
        await client.aclose()

    asyncio.run(scenario())


@pytest.mark.parametrize(
    "response",
    [
        httpx.Response(503, text="unavailable"),
        httpx.Response(302, headers={"Content-Type": "text/html"}, text="<html>redirect</html>"),
        httpx.Response(200, headers={"Content-Type": "application/json"}, json={}),
    ],
)
def test_raw_event_stream_validates_upstream_before_returning(response):
    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(lambda _: response))
        api = RuntimeAPI("http://runtime/api/v1", client=client, token=TEST_TOKEN)
        with pytest.raises(RuntimeError):
            await api.open_event_stream(after=0)
        assert response.is_closed
        await client.aclose()

    asyncio.run(scenario())
