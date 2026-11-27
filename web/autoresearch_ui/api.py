from __future__ import annotations

from typing import Any

import httpx

from autoresearch_ui.auth import validate_operator_token

MAX_EVENT_ID = 2**63 - 1


class RuntimeAPI:
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None, *, token: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.client = client or httpx.AsyncClient(timeout=httpx.Timeout(30.0, read=None))
        self._owns_client = client is None
        self._headers = {"Authorization": f"Bearer {validate_operator_token(token)}"}

    async def get(self, path: str) -> Any:
        response = await self.client.get(f"{self.base_url}/{path.lstrip('/')}", headers=self._headers)
        return self._decode(response)

    async def post(self, path: str, payload: dict[str, Any] | None = None) -> Any:
        response = await self.client.post(f"{self.base_url}/{path.lstrip('/')}", json=payload, headers=self._headers)
        return self._decode(response)

    async def open_event_stream(
        self, *, last_event_id: str | int | None = None, after: str | int | None = None
    ) -> httpx.Response:
        headers = dict(self._headers)
        if last_event_id is not None:
            headers["Last-Event-ID"] = self._event_cursor(last_event_id)
        params = {}
        if after is not None:
            params["after"] = self._event_cursor(after)
        request = self.client.build_request("GET", f"{self.base_url}/events/stream", params=params, headers=headers)
        request.headers.pop("Cookie", None)
        response = await self.client.send(request, stream=True)
        try:
            if response.status_code != 200:
                raise RuntimeError(f"Runtime event stream returned HTTP {response.status_code}")
            if not response.headers.get("Content-Type", "").lower().startswith("text/event-stream"):
                raise RuntimeError("Runtime event stream returned an invalid content type")
            return response
        except Exception:
            await response.aclose()
            raise

    async def control(self, action: str) -> Any:
        allowed = {"start", "stop", "pause", "resume", "finalize"}
        if action not in allowed:
            raise ValueError(f"Unsupported control action: {action}")
        return await self.post(f"run/{action}")

    async def chat(self, message: str, directive: bool = False) -> Any:
        if not message.strip():
            raise ValueError("A message is required")
        return await self.post("chat", {"message": message, "directive": directive})

    async def aclose(self) -> None:
        if self._owns_client:
            await self.client.aclose()

    @staticmethod
    def _event_cursor(value: str | int) -> str:
        if isinstance(value, bool) or not isinstance(value, str | int):
            raise ValueError("Event cursor must be a non-negative integer")
        text = str(value)
        if not text.isascii() or not text.isdigit() or not text:
            raise ValueError("Event cursor must be a non-negative integer")
        cursor = int(text)
        if cursor > MAX_EVENT_ID:
            raise ValueError("Event cursor is too large")
        return str(cursor)

    @staticmethod
    def _decode(response: httpx.Response) -> Any:
        if response.is_error:
            detail = response.text[:1000]
            raise RuntimeError(f"Runtime API returned HTTP {response.status_code}: {detail}")
        if not response.content:
            return {}
        return response.json()
