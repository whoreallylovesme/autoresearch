#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

python3 - <<'PY'
import re
import subprocess
from pathlib import Path, PurePosixPath

wrappers = [
    "demo.sh",
    "control-center.sh",
    "browser-smoke.sh",
    "local-model-demo.sh",
    "compose-smoke.sh",
    "native-kimi-smoke.sh",
]
for wrapper in wrappers:
    text = Path("scripts", wrapper).read_text()
    assert "source scripts/lib/operator-auth.sh" in text, wrapper
    assert "autoresearch_prepare_operator_auth" in text, wrapper
    assert "autoresearch_cleanup_operator_auth" in text, wrapper
    assert "AUTORESEARCH_OPERATOR_TOKEN=" not in text, wrapper
    assert text.index("trap cleanup EXIT INT TERM") < text.index("autoresearch_prepare_operator_auth"), wrapper

for wrapper in ("control-center.sh", "browser-smoke.sh"):
    text = Path("scripts", wrapper).read_text()
    assert 'mkdir -m 0700 "$data_dir/.nicegui"' in text, wrapper
    assert 'NICEGUI_STORAGE_PATH="$data_dir/.nicegui"' in text, wrapper

control_center = Path("scripts/control-center.sh").read_text()
assert "UI login: http://127.0.0.1:8081" in control_center
assert "operator token file: %s" in control_center
assert '"$AUTORESEARCH_OPERATOR_TOKEN_FILE"' in control_center
assert 'cat "$AUTORESEARCH_OPERATOR_TOKEN_FILE"' not in control_center
assert control_center.index("UI login:") < control_center.index("./bin/researchctl run")

compose_smoke = Path("scripts/compose-smoke.sh").read_text()
assert "http://127.0.0.1:${AUTORESEARCH_API_PORT}/healthz" in compose_smoke
assert "curl -fsS http://127.0.0.1:${AUTORESEARCH_API_PORT}/api/v1/" not in compose_smoke

native_smoke = Path("scripts/native-kimi-smoke.sh").read_text()
assert 'docker exec "$daemon" curl -fsS' not in native_smoke
assert "./bin/researchctl status" in native_smoke
assert "./bin/researchctl agents" in native_smoke
assert '"${compose[@]}" run --rm --no-deps operator-auth-init' in native_smoke
assert "cleanup --preserve-operator-auth" in native_smoke
assert native_smoke.index("autoresearch_prepare_operator_auth") < native_smoke.index("\ncleanup --preserve-operator-auth\n")
assert 'if [ "${1:-}" != "--preserve-operator-auth" ]' in native_smoke
assert '"${compose[@]}" build researchd' in native_smoke
assert native_smoke.index('"${compose[@]}" build researchd') < native_smoke.index('"${compose[@]}" run --rm --no-deps researchd')
assert "curl -sS -o /dev/null -w '%{http_code}'" in native_smoke
assert '[ "$unauthenticated_status" != 401 ]' in native_smoke

expected_unit_setting = "Environment=AUTORESEARCH_OPERATOR_TOKEN_FILE=/etc/autoresearch/operator.token"
for unit in ("researchd.service", "autoresearch-ui.service"):
    assert expected_unit_setting in Path("deploy", unit).read_text(), unit
assert "Environment=NICEGUI_STORAGE_PATH=/tmp/nicegui" in Path("deploy/autoresearch-ui.service").read_text()
example = Path("deploy/autoresearch.env.example").read_text()
assert "AUTORESEARCH_OPERATOR_TOKEN_FILE=/etc/autoresearch/operator.token" in example
assert "AUTORESEARCH_OPERATOR_TOKEN=" not in example
assert "NICEGUI_STORAGE_PATH=/tmp/nicegui" in example
assert "AUTORESEARCH_UI_ALLOWED_HOSTS=localhost:8081,127.0.0.1:8081" in example
assert "AUTORESEARCH_UI_ALLOWED_ORIGINS=http://127.0.0.1:8081" in example
assert "AUTORESEARCH_UI_SESSION_MAX_AGE=28800" in example

def git_ignores(filename):
    result = subprocess.run(
        ["git", "check-ignore", "--no-index", "-q", "--", filename],
        check=False,
        capture_output=True,
        text=True,
    )
    assert result.returncode in (0, 1), result.stderr
    return result.returncode == 0


assert git_ignores("deploy/autoresearch.env")
for filename in (".env.example", "deploy/autoresearch.env.example"):
    assert not git_ignores(filename), filename

ignore_patterns = set(Path(".dockerignore").read_text().splitlines())
assert "**/*.env" in ignore_patterns
assert PurePosixPath("deploy/autoresearch.env").match("**/*.env")
for filename in (".env.example", "deploy/autoresearch.env.example"):
    assert not PurePosixPath(filename).match("**/*.env"), filename
for pattern in (".env", ".env.*", "**/.env", "**/.env.*", "*.env", "**/*.env", "*.token", "**/*.token", "*.secret", "**/*.secret", "private", "**/private", "secrets", "**/secrets"):
    assert pattern in ignore_patterns, pattern
for ignore_file in (".gitignore", ".dockerignore"):
    assert ".nicegui" in set(Path(ignore_file).read_text().splitlines()), ignore_file
assert "**/.nicegui" in ignore_patterns, "nested web/.nicegui must be excluded from Docker build context"
for ignore_file in (".gitignore", ".dockerignore"):
    patterns = set(Path(ignore_file).read_text().splitlines())
    for pattern in ("*.token", "*.tokens", "*.secret", "*.secrets", "operator-token", "operator.token"):
        assert pattern in patterns, (ignore_file, pattern)

makefile = Path("Makefile").read_text()
assert "auth-test:" in makefile
for test_script in ("operator_auth_test.sh", "compose_auth_test.sh", "script_auth_static_test.sh"):
    assert f"bash scripts/tests/{test_script}" in makefile, test_script
assert "bash tests/local_model_cache_test.sh" in makefile

ci = Path(".github/workflows/ci.yml").read_text()
assert "make auth-test" in ci
for existing_job in ("go", "python", "repository", "browser", "compose", "native-kimi"):
    assert f"\n  {existing_job}:\n" in ci, existing_job
assert "make auth-test" in ci.split("\n  deployment-auth:\n", 1)[1]
secret_guard = re.search(r"grep -E '([^']+)'", ci)
assert secret_guard
secret_pattern = secret_guard.group(1)
for filename in ("deploy/autoresearch.env", "operator-token", "nested/operator-token", "operator.token", "nested/session.tokens", "operator.secret", "nested/session.secrets"):
    assert re.search(secret_pattern, filename), filename
for filename in (".env.example", "deploy/autoresearch.env.example", "docs/operator-token.md", "docs/operator.token.md", "docs/authentication.md", "docs/environment.md", "scripts/lib/operator-auth.sh"):
    assert not re.search(secret_pattern, filename), filename
PY
printf 'ok - wrappers, systemd examples, and Docker ignores preserve operator authentication\n'
