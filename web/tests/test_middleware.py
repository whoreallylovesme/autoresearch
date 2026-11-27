from __future__ import annotations

import importlib

import pytest
from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse, PlainTextResponse, Response
from fastapi.testclient import TestClient
from starlette.middleware.sessions import SessionMiddleware
from starlette.websockets import WebSocket, WebSocketDisconnect

TEST_TOKEN = "unit-test-operator-token-7f3a9c5e1b8d4f6a2c0e"
TEST_SECRET = "unit-test-storage-secret-4e9a2c7f1b6d8a3e5c0f"


def auth_module():
    return importlib.import_module("autoresearch_ui.auth")


def deployment(**overrides):
    environ = {
        "AUTORESEARCH_UI_HOST": "127.0.0.1",
        "AUTORESEARCH_UI_PORT": "8081",
        **overrides,
    }
    return auth_module().DeploymentSettings.from_environment(environ)


def test_loopback_http_defaults_are_exact_and_use_finite_insecure_local_cookie():
    settings = deployment()

    assert settings.allowed_hosts == frozenset({"127.0.0.1:8081"})
    assert settings.allowed_origins == frozenset({"http://127.0.0.1:8081"})
    assert settings.secure_cookies is False
    assert 0 < settings.max_age <= 24 * 60 * 60


def test_secure_deployment_requires_explicit_exact_host_and_https_origins():
    with pytest.raises(ValueError, match="ALLOWED_HOSTS"):
        deployment(AUTORESEARCH_UI_HOST="0.0.0.0")

    settings = deployment(
        AUTORESEARCH_UI_HOST="0.0.0.0",
        AUTORESEARCH_UI_ALLOWED_HOSTS="research.example.test, research.example.test:8443",
        AUTORESEARCH_UI_ALLOWED_ORIGINS="https://research.example.test,https://research.example.test:8443",
    )
    assert settings.allowed_hosts == frozenset({"research.example.test", "research.example.test:8443"})
    assert settings.secure_cookies is True

    with pytest.raises(ValueError, match="HTTPS"):
        deployment(
            AUTORESEARCH_UI_HOST="0.0.0.0",
            AUTORESEARCH_UI_ALLOWED_HOSTS="research.example.test",
            AUTORESEARCH_UI_ALLOWED_ORIGINS="http://research.example.test",
        )
    with pytest.raises(ValueError):
        deployment(AUTORESEARCH_UI_ALLOWED_HOSTS="*")


def make_client(secret=TEST_SECRET):
    auth = auth_module()
    operator = auth.OperatorAuth(TEST_TOKEN, max_age=60)
    settings = auth.DeploymentSettings(
        allowed_hosts=frozenset({"testserver"}),
        allowed_origins=frozenset({"http://testserver"}),
        secure_cookies=False,
        max_age=60,
    )
    sessions = {}
    application = FastAPI()

    @application.get("/health")
    async def health():
        return {"status": "ok"}

    @application.get("/page")
    async def page():
        return PlainTextResponse("protected page")

    @application.get("/runtime/data")
    async def runtime_data():
        return JSONResponse({"secret": "runtime"})

    @application.post("/action")
    async def action():
        return Response(status_code=204)

    @application.post("/login")
    async def establish_test_session(request: Request):
        request.session["id"] = "test-session"
        sessions["test-session"] = operator.new_session()
        return Response(status_code=204)

    @application.websocket("/ws")
    async def websocket(websocket: WebSocket):
        await websocket.accept()
        await websocket.send_text("connected")
        await websocket.close()

    def session_for(request: Request):
        session_id = request.session.get("id")
        return sessions.setdefault(session_id, {}) if session_id else {}

    guarded = auth.OperatorAuthMiddleware(
        application,
        operator=operator,
        settings=settings,
        session_for=session_for,
    )
    return TestClient(SessionMiddleware(guarded, secret_key=secret, same_site="strict", https_only=False)), sessions


def authenticate(client: TestClient):
    response = client.post("/login", headers={"Origin": "http://testserver"})
    assert response.status_code == 204


def test_only_health_login_and_necessary_static_assets_are_public():
    client, _ = make_client()

    assert client.get("/health").status_code == 200
    assert client.get("/static/theme.css").status_code == 404
    assert client.get("/_nicegui/4.1.0/static/vue.js").status_code == 404
    assert client.get("/_nicegui/4.1.0/libraries/vue.js").status_code == 404

    page = client.get("/page?redirect_to=https://attacker.invalid", follow_redirects=False)
    assert page.status_code in {302, 303}
    assert page.headers["location"] == "/login"
    assert page.headers["cache-control"] == "no-store"
    for path in [
        "/runtime/data",
        "/internal/graph/elements",
        "/runtime/events/stream",
        "/_nicegui_ws/socket.io/?EIO=4&transport=polling",
        "/_nicegui/4.1.0/resources/1/private.js",
        "/_nicegui/4.1.0/dynamic_resources/private",
    ]:
        response = client.get(path)
        assert response.status_code == 401, path
        assert response.headers["content-type"].startswith("application/json")
        assert response.headers["cache-control"] == "no-store"


def test_authenticated_page_runtime_and_websocket_requests_pass():
    client, sessions = make_client()
    authenticate(client)

    assert client.get("/page").text == "protected page"
    assert client.get("/runtime/data").json() == {"secret": "runtime"}
    assert set(sessions["test-session"]) == {"authenticated", "expires_at"}
    with client.websocket_connect("/ws", headers={"Origin": "http://testserver"}) as socket:
        assert socket.receive_text() == "connected"


def test_expired_session_is_revoked_for_http_and_websocket(monkeypatch):
    client, sessions = make_client()
    authenticate(client)
    sessions["test-session"]["expires_at"] = 0

    assert client.get("/page", follow_redirects=False).status_code in {302, 303}
    assert client.get("/runtime/data").status_code == 401
    with pytest.raises(WebSocketDisconnect):
        with client.websocket_connect("/ws", headers={"Origin": "http://testserver"}):
            pass


def test_host_origin_and_csrf_checks_apply_to_http_and_websocket():
    client, _ = make_client()
    authenticate(client)

    assert client.get("/page", headers={"Host": "attacker.invalid"}).status_code == 400
    assert client.get("/page", headers={"Origin": "https://attacker.invalid"}).status_code == 403
    assert client.post("/action").status_code == 403
    assert client.post("/action", headers={"Origin": "https://attacker.invalid"}).status_code == 403
    assert client.post("/action", headers={"Origin": "http://testserver"}).status_code == 204
    with pytest.raises(WebSocketDisconnect):
        with client.websocket_connect("/ws"):
            pass
    with pytest.raises(WebSocketDisconnect):
        with client.websocket_connect("/ws", headers={"Origin": "https://attacker.invalid"}):
            pass


def test_new_startup_secret_invalidates_existing_session_cookie():
    old_client, _ = make_client()
    authenticate(old_client)
    old_cookie = old_client.cookies.get("session")

    restarted_client, _ = make_client(secret="new-startup-secret-9a8b7c6d5e4f3210abcde")
    restarted_client.cookies.set("session", old_cookie, domain="testserver", path="/")

    response = restarted_client.get("/page", follow_redirects=False)
    assert response.status_code == 303
    assert response.headers["location"] == "/login"


@pytest.mark.parametrize("host", ["*", "*.example.test", "research.example.test,*"])
def test_wildcard_hosts_fail_startup(host):
    with pytest.raises(ValueError, match="Wildcard|exact"):
        deployment(AUTORESEARCH_UI_ALLOWED_HOSTS=host)


@pytest.mark.parametrize("origin", ["*", "https://*", "https://*.example.test"])
def test_wildcard_origins_fail_startup(origin):
    with pytest.raises(ValueError, match="Wildcard|exact"):
        deployment(
            AUTORESEARCH_UI_HOST="0.0.0.0",
            AUTORESEARCH_UI_ALLOWED_HOSTS="research.example.test",
            AUTORESEARCH_UI_ALLOWED_ORIGINS=origin,
        )
