from __future__ import annotations

import hashlib
import hmac
import ipaddress
import math
import os
import re
import stat
import time
from collections import OrderedDict, deque
from collections.abc import Callable, Mapping, MutableMapping
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from starlette.requests import HTTPConnection
from starlette.responses import JSONResponse, PlainTextResponse, RedirectResponse
from starlette.types import ASGIApp, Message, Receive, Scope, Send
from starlette.websockets import WebSocket

TOKEN_ENV = "AUTORESEARCH_OPERATOR_TOKEN"
TOKEN_FILE_ENV = "AUTORESEARCH_OPERATOR_TOKEN_FILE"
MAX_TOKEN_BYTES = 4096
MAX_LOGIN_BODY_BYTES = 8192


def validate_operator_token(token: str) -> str:
    if not isinstance(token, str):
        raise ValueError("Operator token must be text")
    if len(token) < 32 or len(set(token)) < 10:
        raise ValueError("Operator token must be a high-entropy machine-generated value")
    if any(not 33 <= ord(character) <= 126 for character in token):
        raise ValueError("Operator token must be a single line of printable ASCII without whitespace")
    return token


def _read_token_file(path: str) -> str:
    if not stat.S_ISREG(Path(path).lstat().st_mode):
        raise ValueError("Operator token file must be an owner-only regular file")
    flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags)
    try:
        metadata = os.fstat(descriptor)
        if not stat.S_ISREG(metadata.st_mode):
            raise ValueError("Operator token file must be an owner-only regular file")
        if metadata.st_uid != os.geteuid() or stat.S_IMODE(metadata.st_mode) & 0o077:
            raise ValueError("Operator token file must be owner-only")
        with os.fdopen(descriptor, "rb") as source:
            descriptor = -1
            raw = source.read(MAX_TOKEN_BYTES + 1)
    finally:
        if descriptor >= 0:
            os.close(descriptor)
    if len(raw) > MAX_TOKEN_BYTES:
        raise ValueError("Operator token file is too large")
    if raw.endswith(b"\n"):
        raw = raw[:-1]
        if raw.endswith(b"\r"):
            raw = raw[:-1]
    try:
        return raw.decode("ascii")
    except UnicodeDecodeError as error:
        raise ValueError("Operator token must be ASCII") from error


def load_operator_token(environ: Mapping[str, str] | None = None) -> str:
    environ = os.environ if environ is None else environ
    token = environ.get(TOKEN_ENV)
    token_file = environ.get(TOKEN_FILE_ENV)
    if (token is None) == (token_file is None):
        raise ValueError(f"Configure exactly one of {TOKEN_ENV} or {TOKEN_FILE_ENV}")
    candidate = token if token is not None else _read_token_file(token_file or "")
    return validate_operator_token(candidate)


class OperatorAuth:
    def __init__(self, token: str, *, max_age: float = 8 * 60 * 60, clock: Callable[[], float] = time.time) -> None:
        validate_operator_token(token)
        if not math.isfinite(max_age) or not 0 < max_age <= 24 * 60 * 60:
            raise ValueError("Session max_age must be finite and between one second and 24 hours")
        self.max_age = float(max_age)
        self._digest = hashlib.sha256(token.encode("ascii")).digest()
        self._clock = clock

    def verify(self, candidate: str) -> bool:
        if not isinstance(candidate, str):
            return False
        candidate_digest = hashlib.sha256(candidate.encode("utf-8")).digest()
        return hmac.compare_digest(self._digest, candidate_digest)

    def new_session(self) -> dict[str, bool | float]:
        return {"authenticated": True, "expires_at": self._clock() + self.max_age}

    def is_authenticated(self, session: Mapping[str, Any]) -> bool:
        if session.get("authenticated") is not True:
            return False
        expires_at = session.get("expires_at")
        if isinstance(expires_at, bool) or not isinstance(expires_at, int | float):
            return False
        remaining = float(expires_at) - self._clock()
        return math.isfinite(remaining) and 0 < remaining <= self.max_age


class LoginThrottle:
    def __init__(
        self,
        *,
        max_attempts: int = 5,
        window_seconds: float = 60,
        max_clients: int = 1024,
        clock: Callable[[], float] = time.monotonic,
    ) -> None:
        if min(max_attempts, max_clients) <= 0 or not math.isfinite(window_seconds) or window_seconds <= 0:
            raise ValueError("Login throttle limits must be positive")
        self.max_attempts = max_attempts
        self.window_seconds = window_seconds
        self.max_clients = max_clients
        self._clock = clock
        self._failures: OrderedDict[str, deque[float]] = OrderedDict()

    def _prune(self, key: str) -> deque[float]:
        failures = self._failures.get(key, deque())
        cutoff = self._clock() - self.window_seconds
        while failures and failures[0] <= cutoff:
            failures.popleft()
        if failures:
            self._failures[key] = failures
            self._failures.move_to_end(key)
        else:
            self._failures.pop(key, None)
        return failures

    def allow(self, key: str) -> bool:
        return len(self._prune(key)) < self.max_attempts

    def record_failure(self, key: str) -> None:
        failures = self._prune(key)
        if not failures and key not in self._failures:
            while len(self._failures) >= self.max_clients:
                self._failures.popitem(last=False)
            self._failures[key] = failures
        if len(failures) < self.max_attempts:
            failures.append(self._clock())

    def reset(self, key: str) -> None:
        self._failures.pop(key, None)

    def __len__(self) -> int:
        for key in list(self._failures):
            self._prune(key)
        return len(self._failures)


def prepare_storage_path(environ: Mapping[str, str] | None = None) -> Path:
    environ = os.environ if environ is None else environ
    path = Path(environ.get("NICEGUI_STORAGE_PATH", ".nicegui")).expanduser().absolute()
    path.mkdir(mode=0o700, parents=True, exist_ok=True)
    metadata = path.lstat()
    if not stat.S_ISDIR(metadata.st_mode) or stat.S_ISLNK(metadata.st_mode):
        raise ValueError("NICEGUI_STORAGE_PATH must be a real directory, not a symlink")
    if metadata.st_uid != os.geteuid():
        raise ValueError("NICEGUI_STORAGE_PATH must be owned by the UI user")
    path.chmod(0o700)
    return path


def clear_authentication(session: MutableMapping[str, Any]) -> None:
    session.pop("authenticated", None)
    session.pop("expires_at", None)


DEFAULT_SESSION_MAX_AGE = 8 * 60 * 60
UNSAFE_METHODS = {"POST", "PUT", "PATCH", "DELETE"}
PUBLIC_FRAMEWORK_ASSET = re.compile(r"^/_nicegui/[^/]+/(?:static|libraries|components|esm)/")


class LoginBodyTooLarge(Exception):
    pass


def limited_login_receive(receive: Receive) -> Receive:
    total = 0

    async def limited() -> Message:
        nonlocal total
        message = await receive()
        if message["type"] == "http.request":
            total += len(message.get("body", b""))
            if total > MAX_LOGIN_BODY_BYTES:
                raise LoginBodyTooLarge
        return message

    return limited


def _csv(value: str) -> list[str]:
    entries = value.split(",")
    if not entries or any(not entry.strip() for entry in entries):
        raise ValueError("Allowlists must contain non-empty comma-separated entries")
    return [entry.strip() for entry in entries]


def _validate_host(host: str) -> str:
    host = host.lower()
    if "*" in host or any(character.isspace() for character in host) or any(c in host for c in "/@?#"):
        raise ValueError("Allowed hosts must be exact host[:port] entries")
    parsed = urlsplit(f"//{host}")
    if not parsed.hostname or parsed.path:
        raise ValueError("Allowed hosts must be exact host[:port] entries")
    if parsed.port is not None and not 0 < parsed.port <= 65535:
        raise ValueError("Allowed host port is invalid")
    return host


def _host_with_port(host: str, port: int) -> str:
    return f"[{host}]:{port}" if ":" in host and not host.startswith("[") else f"{host}:{port}"


def _is_loopback_hostname(hostname: str) -> bool:
    if hostname.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(hostname).is_loopback
    except ValueError:
        return False


def _validate_origin(origin: str) -> str:
    if "*" in origin:
        raise ValueError("Wildcard origins are not allowed; configure exact origins")
    parsed = urlsplit(origin)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.hostname
        or parsed.username
        or parsed.password
        or parsed.path
        or parsed.query
        or parsed.fragment
        or any(character.isspace() for character in origin)
    ):
        raise ValueError("Allowed origins must be exact HTTP(S) origins without paths")
    if parsed.port is not None and not 0 < parsed.port <= 65535:
        raise ValueError("Allowed origin port is invalid")
    return f"{parsed.scheme}://{parsed.netloc.lower()}"


@dataclass(frozen=True)
class DeploymentSettings:
    allowed_hosts: frozenset[str]
    allowed_origins: frozenset[str]
    secure_cookies: bool
    max_age: float = DEFAULT_SESSION_MAX_AGE

    @classmethod
    def from_environment(cls, environ: Mapping[str, str] | None = None) -> DeploymentSettings:
        environ = os.environ if environ is None else environ
        host = environ.get("AUTORESEARCH_UI_HOST", "127.0.0.1")
        try:
            port = int(environ.get("AUTORESEARCH_UI_PORT", "8081"))
            if not 0 < port <= 65535:
                raise ValueError
            max_age = float(environ.get("AUTORESEARCH_UI_SESSION_MAX_AGE", DEFAULT_SESSION_MAX_AGE))
            if not math.isfinite(max_age) or not 0 < max_age <= 24 * 60 * 60:
                raise ValueError
        except ValueError as error:
            raise ValueError("Invalid UI port or session max_age") from error

        configured_hosts = environ.get("AUTORESEARCH_UI_ALLOWED_HOSTS")
        if configured_hosts is None:
            if not _is_loopback_hostname(host):
                raise ValueError("AUTORESEARCH_UI_ALLOWED_HOSTS is required for non-loopback deployments")
            hosts = {_host_with_port(host, port)}
        else:
            hosts = {_validate_host(entry) for entry in _csv(configured_hosts)}

        configured_origins = environ.get("AUTORESEARCH_UI_ALLOWED_ORIGINS")
        if configured_origins is None:
            if any(not _is_loopback_hostname(urlsplit(f"//{entry}").hostname or "") for entry in hosts):
                raise ValueError("AUTORESEARCH_UI_ALLOWED_ORIGINS is required for non-loopback deployments")
            origins = {f"http://{entry}" for entry in hosts}
        else:
            origins = {_validate_origin(entry) for entry in _csv(configured_origins)}

        for origin in origins:
            parsed = urlsplit(origin)
            if parsed.scheme == "http" and not _is_loopback_hostname(parsed.hostname or ""):
                raise ValueError("Non-loopback UI origins must use HTTPS")
        secure_cookies = any(urlsplit(origin).scheme == "https" for origin in origins)
        return cls(frozenset(hosts), frozenset(origins), secure_cookies, max_age)


class OperatorAuthMiddleware:
    def __init__(
        self,
        app: ASGIApp,
        *,
        operator: OperatorAuth,
        settings: DeploymentSettings,
        session_for: Callable[[HTTPConnection], MutableMapping[str, Any]],
    ) -> None:
        self.app = app
        self.operator = operator
        self.settings = settings
        self.session_for = session_for

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        if scope["type"] not in {"http", "websocket"}:
            await self.app(scope, receive, send)
            return
        connection = HTTPConnection(scope)
        path = scope.get("path", "/")
        host = connection.headers.get("host", "").lower()
        origin = connection.headers.get("origin", "").lower()
        if host not in self.settings.allowed_hosts:
            await self._reject(scope, receive, send, 400, "Invalid host")
            return
        if origin and origin not in self.settings.allowed_origins:
            await self._reject(scope, receive, send, 403, "Invalid origin")
            return
        if (scope["type"] == "websocket" or scope.get("method") in UNSAFE_METHODS) and not origin:
            await self._reject(scope, receive, send, 403, "Origin required")
            return
        if self._is_public(path, scope.get("method", "")):
            if path == "/login" and scope.get("method") == "POST":
                content_length = connection.headers.get("content-length")
                if content_length and (not content_length.isdigit() or int(content_length) > MAX_LOGIN_BODY_BYTES):
                    await self._reject(scope, receive, send, 413, "Request body too large")
                    return
                try:
                    await self.app(scope, limited_login_receive(receive), send)
                except LoginBodyTooLarge:
                    await self._reject(scope, receive, send, 413, "Request body too large")
                return
            await self.app(scope, receive, send)
            return
        session = self.session_for(connection)
        if self.operator.is_authenticated(session):
            await self.app(scope, receive, send)
            return
        clear_authentication(session)
        await self._reject(scope, receive, send, 401, "Authentication required")

    @staticmethod
    def _is_public(path: str, method: str) -> bool:
        if path == "/health":
            return method == "GET"
        if path == "/login":
            return method in {"GET", "POST"}
        if method != "GET":
            return False
        return path == "/favicon.ico" or path.startswith("/static/") or bool(PUBLIC_FRAMEWORK_ASSET.match(path))

    async def _reject(self, scope: Scope, receive: Receive, send: Send, status: int, detail: str) -> None:
        if scope["type"] == "websocket":
            await WebSocket(scope, receive, send).close(code=1008)
            return
        path = scope.get("path", "/")
        accept = HTTPConnection(scope).headers.get("accept", "")
        wants_json = (
            path.startswith(("/runtime/", "/internal/", "/_nicegui/", "/_nicegui_ws"))
            or "application/json" in accept
            or "text/event-stream" in accept
        )
        headers = {"Cache-Control": "no-store"}
        if status == 401 and not wants_json:
            response = RedirectResponse("/login", status_code=303, headers=headers)
        elif status == 401:
            response = JSONResponse({"detail": detail}, status_code=status, headers=headers)
        else:
            response = PlainTextResponse(detail, status_code=status, headers=headers)
        await response(scope, receive, send)
