import asyncio
import json

import httpx

from autoresearch_ui.api import RuntimeAPI


def test_runtime_api_uses_versioned_endpoints_and_chat():
    requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path == "/api/v1/chat":
            return httpx.Response(200, json={"response": "Current state"})
        return httpx.Response(200, json={"state": "running", "objective": "Research"})

    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        api = RuntimeAPI("http://runtime/api/v1", client=client)
        status = await api.get("run/status")
        response = await api.chat("Explain", directive=False)
        await client.aclose()
        assert status["objective"] == "Research"
        assert response["response"] == "Current state"

    asyncio.run(scenario())
    assert requests[0].url.path == "/api/v1/run/status"
    assert requests[1].url.path == "/api/v1/chat"
    assert json.loads(requests[1].content) == {"message": "Explain", "directive": False}


def test_runtime_api_surfaces_safe_error():
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(503, text="provider unavailable")

    async def scenario():
        client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
        api = RuntimeAPI("http://runtime/api/v1", client=client)
        try:
            await api.get("run/status")
        except RuntimeError as error:
            assert "503" in str(error)
            assert "provider unavailable" in str(error)
        else:
            raise AssertionError("expected RuntimeError")
        await client.aclose()

    asyncio.run(scenario())
