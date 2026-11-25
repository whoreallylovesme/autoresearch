from __future__ import annotations

from typing import Any

import httpx


class RuntimeAPI:
    def __init__(self, base_url: str, client: httpx.AsyncClient | None = None) -> None:
        self.base_url = base_url.rstrip("/")
        self.client = client or httpx.AsyncClient(timeout=httpx.Timeout(30.0, read=None))
        self._owns_client = client is None

    async def get(self, path: str) -> Any:
        response = await self.client.get(f"{self.base_url}/{path.lstrip('/')}")
        return self._decode(response)

    async def post(self, path: str, payload: dict[str, Any] | None = None) -> Any:
        response = await self.client.post(f"{self.base_url}/{path.lstrip('/')}", json=payload)
        return self._decode(response)

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
    def _decode(response: httpx.Response) -> Any:
        if response.is_error:
            detail = response.text[:1000]
            raise RuntimeError(f"Runtime API returned HTTP {response.status_code}: {detail}")
        if not response.content:
            return {}
        return response.json()
