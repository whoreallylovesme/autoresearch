from __future__ import annotations

import hashlib
import hmac
import importlib
import os
import stat
from pathlib import Path

import pytest

TEST_TOKEN = "unit-test-operator-token-7f3a9c5e1b8d4f6a2c0e"


def auth_module():
    return importlib.import_module("autoresearch_ui.auth")


def test_loads_exactly_one_operator_token_from_environment():
    auth = auth_module()

    assert auth.load_operator_token({"AUTORESEARCH_OPERATOR_TOKEN": TEST_TOKEN}) == TEST_TOKEN
    with pytest.raises(ValueError, match="exactly one"):
        auth.load_operator_token({})
    with pytest.raises(ValueError, match="exactly one"):
        auth.load_operator_token(
            {
                "AUTORESEARCH_OPERATOR_TOKEN": TEST_TOKEN,
                "AUTORESEARCH_OPERATOR_TOKEN_FILE": "/tmp/operator-token",
            }
        )


def test_loads_owner_only_regular_token_file(tmp_path: Path):
    auth = auth_module()
    token_file = tmp_path / "operator-token"
    token_file.write_text(TEST_TOKEN + "\n")
    token_file.chmod(0o600)

    assert auth.load_operator_token({"AUTORESEARCH_OPERATOR_TOKEN_FILE": str(token_file)}) == TEST_TOKEN


@pytest.mark.parametrize("mode", [0o640, 0o604, 0o666])
def test_rejects_token_files_accessible_to_group_or_others(tmp_path: Path, mode: int):
    auth = auth_module()
    token_file = tmp_path / "operator-token"
    token_file.write_text(TEST_TOKEN)
    token_file.chmod(mode)

    with pytest.raises(ValueError, match="owner-only"):
        auth.load_operator_token({"AUTORESEARCH_OPERATOR_TOKEN_FILE": str(token_file)})


def test_rejects_symlink_token_file(tmp_path: Path):
    auth = auth_module()
    target = tmp_path / "target"
    target.write_text(TEST_TOKEN)
    target.chmod(0o600)
    token_file = tmp_path / "operator-token"
    token_file.symlink_to(target)

    with pytest.raises(ValueError, match="regular"):
        auth.load_operator_token({"AUTORESEARCH_OPERATOR_TOKEN_FILE": str(token_file)})


@pytest.mark.parametrize(
    "token",
    [
        "",
        "short",
        "a" * 64,
        TEST_TOKEN + "\nsecond-line",
        " leading-" + TEST_TOKEN,
        "trailing-" + TEST_TOKEN + " ",
    ],
)
def test_rejects_low_entropy_or_malformed_tokens(token: str):
    auth = auth_module()

    with pytest.raises(ValueError):
        auth.validate_operator_token(token)


def test_token_verification_uses_fixed_length_digests(monkeypatch):
    auth = auth_module()
    calls = []
    original_compare = hmac.compare_digest

    def record_compare(left, right):
        calls.append((left, right))
        return original_compare(left, right)

    monkeypatch.setattr(auth.hmac, "compare_digest", record_compare)
    operator = auth.OperatorAuth(TEST_TOKEN)

    assert operator.verify(TEST_TOKEN)
    assert not operator.verify("x")
    assert len(calls) == 2
    assert all(len(left) == 32 and len(right) == 32 for left, right in calls)
    assert calls[0][0] == hashlib.sha256(TEST_TOKEN.encode()).digest()


def test_session_state_contains_only_authentication_and_finite_expiry():
    auth = auth_module()
    now = [1000.0]
    operator = auth.OperatorAuth(TEST_TOKEN, max_age=60, clock=lambda: now[0])

    session = operator.new_session()

    assert session == {"authenticated": True, "expires_at": 1060.0}
    assert operator.is_authenticated(session)
    now[0] = 1060.0
    assert not operator.is_authenticated(session)
    assert not operator.is_authenticated({"authenticated": True, "expires_at": 2000.0})
    assert not operator.is_authenticated({"authenticated": 1, "expires_at": 1001.0})
    assert not operator.is_authenticated({})


def test_login_throttling_is_bounded_and_expires():
    auth = auth_module()
    now = [100.0]
    throttle = auth.LoginThrottle(max_attempts=3, window_seconds=30, max_clients=2, clock=lambda: now[0])

    for _ in range(3):
        assert throttle.allow("first")
        throttle.record_failure("first")
    assert not throttle.allow("first")
    assert throttle.allow("second")
    throttle.record_failure("second")
    assert throttle.allow("third")
    throttle.record_failure("third")
    assert len(throttle) == 2

    throttle.reset("third")
    assert len(throttle) == 1
    now[0] = 131.0
    assert throttle.allow("first")


def test_private_storage_path_is_created_without_group_or_other_access(tmp_path: Path, monkeypatch):
    auth = auth_module()
    storage_path = tmp_path / "private-storage"
    monkeypatch.delenv("NICEGUI_STORAGE_PATH", raising=False)

    result = auth.prepare_storage_path({"NICEGUI_STORAGE_PATH": str(storage_path)})

    assert result == storage_path
    assert stat.S_IMODE(storage_path.stat().st_mode) == 0o700
    assert os.environ.get("NICEGUI_STORAGE_PATH") is None


def test_login_throttle_caps_failure_history():
    auth = auth_module()
    throttle = auth.LoginThrottle(max_attempts=3, window_seconds=60)

    for _ in range(100):
        assert throttle.allow("repeated") in {True, False}
        throttle.record_failure("repeated")

    assert not throttle.allow("repeated")
    assert len(throttle._failures["repeated"]) == 3
