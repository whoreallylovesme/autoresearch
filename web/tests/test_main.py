from __future__ import annotations

import asyncio
import re
import time
from urllib.parse import urlencode

import httpx
import pytest
from fastapi import Request
from fastapi.testclient import TestClient
from nicegui.storage import request_contextvar, set_storage_secret

import autoresearch_ui.main as main
from autoresearch_ui.main import app
from tests.conftest import TEST_TOKEN

TEST_SECRET = "unit-test-storage-secret-4e9a2c7f1b6d8a3e5c0f"
ORIGIN = {"Origin": "http://127.0.0.1"}


async def inactive_session():
    return False


class EventStream(httpx.AsyncByteStream):
    async def __aiter__(self):
        yield b"id: 8\ndata: {}\n\n"


@pytest.fixture(scope="session", autouse=True)
def configure_test_sessions():
    set_storage_secret(
        getattr(main, "STORAGE_SECRET", TEST_SECRET),
        getattr(main, "session_middleware_kwargs", lambda: {"max_age": 60})(),
    )


@pytest.fixture(autouse=True)
def reset_auth_state():
    app.storage._users.clear()
    if hasattr(main, "login_throttle"):
        main.login_throttle.reset("testclient")


def client() -> TestClient:
    return TestClient(app, base_url="http://127.0.0.1", follow_redirects=False)


def login(test_client: TestClient, token: str = TEST_TOKEN):
    return test_client.post("/login", data={"operator_token": token}, headers=ORIGIN)


def authenticated_session():
    return next(session for session in app.storage._users.values() if session.get("authenticated") is True)


def authenticated_session_id():
    return next(key for key, session in app.storage._users.items() if session.get("authenticated") is True)


def test_health_endpoint():
    response = client().get("/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_anonymous_pages_redirect_and_runtime_routes_return_401(monkeypatch):
    async def unexpected_get(_):
        raise AssertionError("anonymous request reached RuntimeAPI")

    monkeypatch.setattr(main.runtime, "get", unexpected_get)
    test_client = client()

    for path in ["/", "/agents", "/graph", "/budget", "/experiments", "/events", "/report", "/playback"]:
        response = test_client.get(path, follow_redirects=False)
        assert response.status_code == 303, path
        assert response.headers["location"] == "/login"
        assert "redirect" not in response.headers["location"]
    for path in ["/runtime/graph", "/internal/graph/elements", "/runtime/events/stream", "/future-private.json"]:
        response = test_client.get(path, headers={"Accept": "application/json"})
        assert response.status_code == 401, path
        assert response.json() == {"detail": "Authentication required"}


def test_login_form_is_semantic_accessible_and_has_no_token_or_open_redirect():
    response = client().get("/login?redirect_to=https://attacker.invalid")

    assert response.status_code == 200
    assert response.headers["cache-control"] == "no-store"
    markup = response.text
    assert '<html lang="en">' in markup
    assert '<form method="post" action="/login"' in markup
    assert '<label for="operator-token">Operator token</label>' in markup
    assert re.search(
        r'<input[^>]*id="operator-token"[^>]*name="operator_token"[^>]*type="password"'
        r'[^>]*autocomplete="current-password"[^>]*required',
        markup,
    )
    assert 'id="toggle-password"' in markup
    assert 'type="button"' in markup
    assert 'aria-pressed="false"' in markup
    assert 'aria-controls="operator-token"' in markup
    assert 'aria-describedby="toggle-password-warning"' in markup
    assert 'src="/static/login.js"' in markup
    assert TEST_TOKEN not in markup
    assert "attacker.invalid" not in markup


def test_login_success_stores_only_server_side_auth_state_and_fixed_redirect():
    test_client = client()
    response = login(test_client)

    assert response.status_code == 303
    assert response.headers["location"] == "/"
    cookie = response.headers["set-cookie"]
    assert "httponly" in cookie.lower()
    assert "samesite=strict" in cookie.lower()
    assert "Max-Age=60" in cookie
    assert TEST_TOKEN not in cookie
    assert " Secure" not in cookie
    assert len(app.storage._users) == 2
    session = authenticated_session()
    assert set(session) == {"authenticated", "expires_at"}
    assert session["authenticated"] is True
    assert test_client.get("/login", follow_redirects=False).headers["location"] == "/"


def test_invalid_login_has_generic_error_and_bounded_throttling():
    test_client = client()
    wrong_token = "invalid-operator-token-9f8e7d6c5b4a3210fedcba"
    first = login(test_client, wrong_token)
    second = login(test_client, wrong_token)

    assert first.status_code == 401
    assert first.text == second.text
    assert "Invalid token" in first.text
    assert wrong_token not in first.text
    assert all("authenticated" not in session for session in app.storage._users.values())
    for _ in range(3):
        login(test_client, wrong_token)
    throttled = login(test_client)
    assert throttled.status_code == 429
    assert "Invalid token" in throttled.text
    assert TEST_TOKEN not in throttled.text
    assert all("authenticated" not in session for session in app.storage._users.values())


def test_login_requires_the_exact_origin():
    test_client = client()
    assert login(test_client).status_code == 303
    assert test_client.post("/login", data={"operator_token": TEST_TOKEN}).status_code == 403
    assert (
        test_client.post(
            "/login", data={"operator_token": TEST_TOKEN}, headers={"Origin": "https://attacker.invalid"}
        ).status_code
        == 403
    )


def test_graph_routes_keep_raw_proxy_and_expose_normalized_elements_after_login(monkeypatch):
    snapshot = {
        "nodes": [{"id": "h1", "description": "", "type": "hypothesis", "status": "proposed"}],
        "edges": [{"id": "e1", "from_node": "h1", "to_node": "h1"}],
    }
    paths = []

    async def get(path):
        paths.append(path)
        return snapshot

    monkeypatch.setattr(main.runtime, "get", get)
    test_client = client()
    login(test_client)

    raw_response = test_client.get("/runtime/graph")
    normalized_response = test_client.get("/internal/graph/elements")

    assert raw_response.status_code == 200
    assert raw_response.json() == snapshot
    assert normalized_response.status_code == 200
    assert normalized_response.json() == main.graph_elements(snapshot)
    assert paths == ["graph", "graph"]


def test_expiry_revokes_existing_http_session():
    test_client = client()
    login(test_client)
    session = authenticated_session()
    session["expires_at"] = 0

    assert test_client.get("/", follow_redirects=False).headers["location"] == "/login"
    assert test_client.get("/runtime/graph").status_code == 401
    assert "authenticated" not in session


def test_logout_clears_session_and_disconnects_existing_clients(monkeypatch):
    test_client = client()
    login(test_client)
    session_id = authenticated_session_id()
    disconnected = []

    async def record_disconnect(received_session_id):
        disconnected.append(received_session_id)

    monkeypatch.setattr(main, "disconnect_session_clients", record_disconnect)
    response = test_client.post("/logout", headers=ORIGIN)

    assert response.status_code == 303
    assert response.headers["location"] == "/login"
    assert disconnected == [session_id]
    assert test_client.get("/runtime/graph").status_code == 401
    assert test_client.get("/", follow_redirects=False).headers["location"] == "/login"


def test_sse_proxy_forwards_only_backend_bearer_and_validated_cursors(monkeypatch):
    upstream_requests = []

    def handler(request: httpx.Request) -> httpx.Response:
        upstream_requests.append(request)
        return httpx.Response(200, headers={"Content-Type": "text/event-stream"}, stream=EventStream())

    upstream_client = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(main.runtime, "client", upstream_client)
    test_client = client()
    login(test_client)
    test_client.cookies.set("browser-secret", "discard", domain="127.0.0.1", path="/")

    response = test_client.get(
        "/runtime/events/stream?after=7&unexpected=discard",
        headers={"Last-Event-ID": "6", "Authorization": "Bearer discard"},
    )

    assert response.status_code == 200
    assert response.headers["X-Accel-Buffering"] == "no"
    assert response.text == "id: 8\ndata: {}\n\n"
    request = upstream_requests[0]
    assert request.headers["Authorization"] == f"Bearer {TEST_TOKEN}"
    assert request.headers["Last-Event-ID"] == "6"
    assert request.headers.get("Cookie") is None
    assert dict(request.url.params) == {"after": "7"}
    assert test_client.get("/runtime/events/stream?after=invalid").status_code == 400
    asyncio.run(upstream_client.aclose())


def test_sse_proxy_validates_upstream_status_before_streaming(monkeypatch):
    upstream_client = httpx.AsyncClient(
        transport=httpx.MockTransport(lambda _: httpx.Response(503, text="unavailable"))
    )
    monkeypatch.setattr(main.runtime, "client", upstream_client)
    test_client = client()
    login(test_client)

    response = test_client.get("/runtime/events/stream")

    assert response.status_code == 502
    assert response.json() == {"detail": "Runtime event stream unavailable"}
    asyncio.run(upstream_client.aclose())


def test_registered_clients_are_disconnected_once_and_pruned(monkeypatch):
    disconnected = []

    async def disconnect(socket_id):
        disconnected.append(socket_id)

    monkeypatch.setattr(main.core.sio, "disconnect", disconnect)

    class FakeClient:
        id = "client-1"
        _socket_to_document_id = {"socket-1": "document-1", "socket-2": "document-2"}

        def on_delete(self, callback):
            self.delete_callback = callback

    fake = FakeClient()
    main.register_session_client("session-1", fake)
    asyncio.run(main.disconnect_session_clients("session-1"))
    asyncio.run(main.disconnect_session_clients("session-1"))

    assert disconnected == ["socket-1", "socket-2"]
    fake.delete_callback()
    assert main.session_clients == {}


def test_control_rechecks_authorization_before_runtime_mutation(monkeypatch):
    async def unexpected_control(_):
        raise AssertionError("expired callback reached RuntimeAPI")

    monkeypatch.setattr(main, "require_active_session", inactive_session)
    monkeypatch.setattr(main.runtime, "control", unexpected_control)

    assert asyncio.run(main.control("start")) is None


def test_chat_rechecks_authorization_before_runtime_mutation(monkeypatch):
    async def unexpected_chat(*_):
        raise AssertionError("expired callback reached RuntimeAPI")

    monkeypatch.setattr(main, "require_active_session", inactive_session)
    monkeypatch.setattr(main.runtime, "chat", unexpected_chat)

    assert asyncio.run(main.send_chat("message", False)) is False


def test_sse_proxy_returns_502_for_html_redirect(monkeypatch):
    upstream_client = httpx.AsyncClient(
        transport=httpx.MockTransport(
            lambda _: httpx.Response(302, headers={"Content-Type": "text/html"}, text="<html>redirect</html>")
        )
    )
    monkeypatch.setattr(main.runtime, "client", upstream_client)
    test_client = client()
    login(test_client)

    response = test_client.get("/runtime/events/stream")

    assert response.status_code == 502
    assert response.json() == {"detail": "Runtime event stream unavailable"}
    asyncio.run(upstream_client.aclose())


class PendingUpstream:
    def __init__(self):
        self.first_chunk = b"id: 1\ndata: {}\n\n"
        self.closed = False
        self.waiter = asyncio.Event()

    def aiter_raw(self):
        return self

    def __aiter__(self):
        return self

    async def __anext__(self):
        if self.first_chunk is not None:
            chunk = self.first_chunk
            self.first_chunk = None
            return chunk
        await self.waiter.wait()
        raise StopAsyncIteration

    async def aclose(self):
        self.closed = True
        self.waiter.set()


async def open_pending_stream(monkeypatch):
    upstream = PendingUpstream()

    async def open_event_stream(**_):
        return upstream

    monkeypatch.setattr(main.runtime, "open_event_stream", open_event_stream)

    async def receive():
        await asyncio.Event().wait()

    request = Request(
        {
            "type": "http",
            "asgi": {"version": "3.0"},
            "http_version": "1.1",
            "method": "GET",
            "scheme": "http",
            "path": "/runtime/events/stream",
            "raw_path": b"/runtime/events/stream",
            "query_string": b"",
            "headers": [],
            "client": ("testclient", 123),
            "server": ("127.0.0.1", 80),
            "session": {"id": "stream-session"},
        },
        receive,
    )
    app.storage._users["stream-session"] = main.operator.new_session()
    context = request_contextvar.set(request)
    response = await main.runtime_events(request)
    iterator = response.body_iterator
    assert await asyncio.wait_for(anext(iterator), timeout=0.5) == b"id: 1\ndata: {}\n\n"
    return request, upstream, iterator, context


async def close_pending_stream(iterator, context):
    await iterator.aclose()
    request_contextvar.reset(context)
    app.storage._users.pop("stream-session", None)


def test_open_stream_is_cancelled_by_logout(monkeypatch):
    async def scenario():
        request, upstream, iterator, context = await open_pending_stream(monkeypatch)
        pending = asyncio.create_task(anext(iterator))
        try:
            await asyncio.sleep(0)
            await main.logout(request)
            with pytest.raises(StopAsyncIteration):
                await asyncio.wait_for(pending, timeout=0.5)
            assert upstream.closed
        finally:
            if not pending.done():
                pending.cancel()
            await asyncio.gather(pending, return_exceptions=True)
            await close_pending_stream(iterator, context)

    asyncio.run(scenario())


def test_open_stream_is_cancelled_by_expiry(monkeypatch):
    monkeypatch.setattr(main, "STREAM_REAUTH_INTERVAL", 0.01, raising=False)

    async def scenario():
        request, upstream, iterator, context = await open_pending_stream(monkeypatch)
        pending = asyncio.create_task(anext(iterator))
        try:
            await asyncio.sleep(0)
            app.storage.user["expires_at"] = 0
            with pytest.raises(StopAsyncIteration):
                await asyncio.wait_for(pending, timeout=0.5)
            assert upstream.closed
        finally:
            if not pending.done():
                pending.cancel()
            await asyncio.gather(pending, return_exceptions=True)
            await close_pending_stream(iterator, context)

    asyncio.run(scenario())


def body_chunks(body: bytes, size: int = 64):
    for offset in range(0, len(body), size):
        yield body[offset : offset + size]


def test_login_caps_chunked_urlencoded_body_before_parsing(monkeypatch):
    verified = []

    def verify(candidate):
        verified.append(candidate)
        return False

    monkeypatch.setattr(main.operator, "verify", verify)
    body = b"operator_token=" + TEST_TOKEN.encode() + b"&padding=" + b"x" * 9000

    response = client().post(
        "/login",
        content=body_chunks(body),
        headers={**ORIGIN, "Content-Type": "application/x-www-form-urlencoded"},
    )

    assert response.status_code == 413
    assert response.text == "Request body too large"
    assert verified == []


def test_login_caps_chunked_multipart_body_before_parsing(monkeypatch):
    verified = []

    def verify(candidate):
        verified.append(candidate)
        return False

    monkeypatch.setattr(main.operator, "verify", verify)
    boundary = "autoresearch-boundary"
    body = (
        f'--{boundary}\r\nContent-Disposition: form-data; name="operator_token"\r\n\r\n{TEST_TOKEN}\r\n'
        f'--{boundary}\r\nContent-Disposition: form-data; name="padding"\r\n\r\n'.encode()
        + b"x" * 9000
        + f"\r\n--{boundary}--\r\n".encode()
    )

    response = client().post(
        "/login",
        content=body_chunks(body),
        headers={**ORIGIN, "Content-Type": f"multipart/form-data; boundary={boundary}"},
    )

    assert response.status_code == 413
    assert response.text == "Request body too large"
    assert verified == []


def test_login_accepts_bounded_chunked_multipart():
    boundary = "autoresearch-boundary"
    body = (
        f'--{boundary}\r\nContent-Disposition: form-data; name="operator_token"\r\n\r\n{TEST_TOKEN}\r\n'
        f"--{boundary}--\r\n"
    ).encode()
    test_client = client()

    response = test_client.post(
        "/login",
        content=body_chunks(body, size=17),
        headers={**ORIGIN, "Content-Type": f"multipart/form-data; boundary={boundary}"},
    )

    assert response.status_code == 303
    assert response.headers["location"] == "/"
    assert authenticated_session()["authenticated"] is True


@pytest.mark.parametrize("extra", [[("unexpected", "value")], [("operator_token", TEST_TOKEN)]])
def test_login_rejects_extra_or_duplicate_fields(extra):
    fields = [("operator_token", TEST_TOKEN), *extra]

    response = client().post(
        "/login",
        content=urlencode(fields),
        headers={**ORIGIN, "Content-Type": "application/x-www-form-urlencoded"},
    )

    assert response.status_code == 401
    assert all("authenticated" not in session for session in app.storage._users.values())


def test_stream_open_failure_discards_registration(monkeypatch):
    async def scenario():
        async def fail_open(**_):
            raise httpx.ConnectError("offline")

        monkeypatch.setattr(main.runtime, "open_event_stream", fail_open)
        request = Request(
            {
                "type": "http",
                "asgi": {"version": "3.0"},
                "http_version": "1.1",
                "method": "GET",
                "scheme": "http",
                "path": "/runtime/events/stream",
                "raw_path": b"/runtime/events/stream",
                "query_string": b"",
                "headers": [],
                "client": ("testclient", 123),
                "server": ("127.0.0.1", 80),
                "session": {"id": "stream-session"},
            }
        )
        app.storage._users["stream-session"] = main.operator.new_session()
        context = request_contextvar.set(request)
        try:
            response = await main.runtime_events(request)
            assert response.status_code == 502
            assert response.body == b'{"detail":"Runtime event stream unavailable"}'
            assert main.active_streams == {}
        finally:
            main.cancel_session_streams("stream-session")
            request_contextvar.reset(context)
            app.storage._users.pop("stream-session", None)

    asyncio.run(scenario())


def test_idle_stream_cancellation_reaps_helpers(monkeypatch):
    async def scenario():
        baseline = set(asyncio.all_tasks())
        _, upstream, iterator, context = await open_pending_stream(monkeypatch)
        pending = asyncio.create_task(anext(iterator))
        tracked = set()
        try:
            await asyncio.sleep(0)
            await asyncio.sleep(0)
            tracked = set(asyncio.all_tasks()) - baseline
            active_helpers = [task for task in tracked if task is not pending and not task.done()]
            assert active_helpers

            pending.cancel()
            await asyncio.gather(pending, return_exceptions=True)
            await asyncio.sleep(0)

            assert upstream.closed
            assert [task for task in tracked if not task.done()] == []
        finally:
            remaining = [task for task in tracked if not task.done()]
            for task in remaining:
                task.cancel()
            if remaining:
                await asyncio.gather(*remaining, return_exceptions=True)
            await close_pending_stream(iterator, context)

    asyncio.run(scenario())


def test_connected_client_and_stream_disconnect_on_short_expiry(monkeypatch):
    monkeypatch.setattr(main, "STREAM_REAUTH_INTERVAL", 0.01, raising=False)

    class ConnectedClient:
        id = "expiry-client"
        _socket_to_document_id = {"expiry-socket": "document"}

        def on_delete(self, callback):
            self.delete_callback = callback

    disconnected = []

    async def disconnect(socket_id):
        disconnected.append(socket_id)

    monkeypatch.setattr(main.core.sio, "disconnect", disconnect)

    async def scenario():
        _, upstream, iterator, context = await open_pending_stream(monkeypatch)
        main.register_session_client("stream-session", ConnectedClient())
        pending = asyncio.create_task(anext(iterator))
        try:
            app.storage.user["expires_at"] = time.time() + 0.02
            with pytest.raises(StopAsyncIteration):
                await asyncio.wait_for(pending, timeout=0.5)
            assert upstream.closed
            assert disconnected == ["expiry-socket"]
            assert main.session_clients == {}
            assert main.active_streams == {}
        finally:
            if not pending.done():
                pending.cancel()
            await asyncio.gather(pending, return_exceptions=True)
            main.session_clients.clear()
            await close_pending_stream(iterator, context)

    asyncio.run(scenario())


def test_pending_stream_open_cancellation_discards_registration(monkeypatch):
    async def scenario():
        started = asyncio.Event()

        async def pending_open(**_):
            started.set()
            await asyncio.Event().wait()

        monkeypatch.setattr(main.runtime, "open_event_stream", pending_open)
        request = Request(
            {
                "type": "http",
                "asgi": {"version": "3.0"},
                "http_version": "1.1",
                "method": "GET",
                "scheme": "http",
                "path": "/runtime/events/stream",
                "raw_path": b"/runtime/events/stream",
                "query_string": b"",
                "headers": [],
                "client": ("testclient", 123),
                "server": ("127.0.0.1", 80),
                "session": {"id": "stream-session"},
            }
        )
        app.storage._users["stream-session"] = main.operator.new_session()
        context = request_contextvar.set(request)
        task = asyncio.create_task(main.runtime_events(request))
        try:
            await asyncio.wait_for(started.wait(), timeout=0.5)
            task.cancel()
            with pytest.raises(asyncio.CancelledError):
                await task
            assert main.active_streams == {}
        finally:
            if not task.done():
                task.cancel()
            await asyncio.gather(task, return_exceptions=True)
            main.cancel_session_streams("stream-session")
            request_contextvar.reset(context)
            app.storage._users.pop("stream-session", None)

    asyncio.run(scenario())


def test_cancellation_after_open_closes_upstream(monkeypatch):
    async def scenario():
        upstream = PendingUpstream()

        async def open_event_stream(**_):
            return upstream

        def cancel_requirement():
            raise asyncio.CancelledError

        monkeypatch.setattr(main.runtime, "open_event_stream", open_event_stream)
        monkeypatch.setattr(main, "require_active_session", cancel_requirement)
        request = Request(
            {
                "type": "http",
                "asgi": {"version": "3.0"},
                "http_version": "1.1",
                "method": "GET",
                "scheme": "http",
                "path": "/runtime/events/stream",
                "raw_path": b"/runtime/events/stream",
                "query_string": b"",
                "headers": [],
                "client": ("testclient", 123),
                "server": ("127.0.0.1", 80),
                "session": {"id": "stream-session"},
            }
        )
        app.storage._users["stream-session"] = main.operator.new_session()
        context = request_contextvar.set(request)
        try:
            with pytest.raises(asyncio.CancelledError):
                await main.runtime_events(request)
            assert upstream.closed
            assert main.active_streams == {}
        finally:
            await upstream.aclose()
            main.cancel_session_streams("stream-session")
            request_contextvar.reset(context)
            app.storage._users.pop("stream-session", None)

    asyncio.run(scenario())


def test_login_rotates_session_and_denies_prelogin_cookie(monkeypatch):
    async def graph(_):
        return {}

    monkeypatch.setattr(main.runtime, "get", graph)
    disconnected = []

    async def disconnect(socket_id):
        disconnected.append(socket_id)

    monkeypatch.setattr(main.core.sio, "disconnect", disconnect)

    class AnonymousClient:
        id = "anonymous-client"
        _socket_to_document_id = {"old-socket": "document"}

        def on_delete(self, callback):
            self.delete_callback = callback

    victim = client()
    attacker = client()
    old_cookie = None
    old_session_id = None
    try:
        assert victim.get("/login").status_code == 200
        old_cookie = victim.cookies.get("session")
        old_session_id = next(iter(app.storage._users))
        attacker.cookies.set("session", old_cookie, domain="127.0.0.1", path="/")
        main.register_session_client(old_session_id, AnonymousClient())
        old_stream = main.register_session_stream(old_session_id)

        response = login(victim)
        new_cookie = victim.cookies.get("session")

        assert response.status_code == 303
        assert "set-cookie" in response.headers
        assert new_cookie != old_cookie
        assert TEST_TOKEN not in new_cookie
        assert attacker.get("/runtime/graph").status_code == 401
        assert attacker.get("/", follow_redirects=False).headers["location"] == "/login"
        assert app.storage._users[old_session_id].get("authenticated") is not True
        assert authenticated_session_id() != old_session_id
        assert disconnected == ["old-socket"]
        assert old_stream.is_set()
        assert old_session_id not in main.active_streams
    finally:
        main.session_clients.clear()
        if old_session_id:
            main.cancel_session_streams(old_session_id)
