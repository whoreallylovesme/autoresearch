#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
compose=(docker compose --profile kimi-web -f deploy/docker-compose.yml)

if env -u AUTORESEARCH_OPERATOR_TOKEN -u AUTORESEARCH_OPERATOR_TOKEN_FILE \
  "${compose[@]}" config --quiet >"$test_root/missing.log" 2>&1; then
  printf 'Compose accepted a missing operator token file\n' >&2
  exit 1
fi
printf 'ok - Compose requires an operator token file\n'

secret_file="$test_root/operator.token"
printf 'compose-static-test-only-token\n' > "$secret_file"
chmod 0600 "$secret_file"
AUTORESEARCH_OPERATOR_TOKEN_FILE="$secret_file" \
  "${compose[@]}" config --format json > "$test_root/compose.json"
AUTORESEARCH_OPERATOR_TOKEN_FILE="$secret_file" \
AUTORESEARCH_UI_PORT=18081 \
AUTORESEARCH_UI_SESSION_MAX_AGE=3600 \
  "${compose[@]}" config --format json > "$test_root/compose-custom.json"

python3 - "$test_root/compose.json" "$test_root/compose-custom.json" <<'PY'
import json
import sys
from pathlib import Path

configuration = json.loads(Path(sys.argv[1]).read_text())
custom_configuration = json.loads(Path(sys.argv[2]).read_text())
services = configuration["services"]
expected_secret_path = "/run/autoresearch/operator.token"
expected_secret_mount = "/run/autoresearch"

assert set(configuration.get("secrets", {})) == {"operator-token"}
assert configuration["secrets"]["operator-token"].get("file"), "operator secret must be file-backed"
secret_consumers = {name for name, service in services.items() if service.get("secrets")}
assert secret_consumers == {"operator-auth-init"}, secret_consumers

for name in ("researchd", "ui"):
    environment = services[name].get("environment", {})
    assert environment.get("AUTORESEARCH_OPERATOR_TOKEN_FILE") == expected_secret_path, name
for name, service in services.items():
    environment = service.get("environment", {})
    assert "AUTORESEARCH_OPERATOR_TOKEN" not in environment, name
    if name not in ("researchd", "ui"):
        assert "AUTORESEARCH_OPERATOR_TOKEN_FILE" not in environment, name

bootstrap = services["operator-auth-init"]
assert bootstrap.get("user") == "0:0"
assert bootstrap.get("read_only") is True
assert set(bootstrap.get("cap_drop", [])) == {"ALL"}
assert set(bootstrap.get("cap_add", [])) == {"CHOWN", "DAC_OVERRIDE", "FOWNER"}
assert not bootstrap.get("ports")
bootstrap_command = bootstrap.get("command", "")
if isinstance(bootstrap_command, list):
    bootstrap_command = " ".join(bootstrap_command)
assert "chown 65532:65532" in bootstrap_command
assert "chmod 0400" in bootstrap_command
assert expected_secret_path in bootstrap_command


def mount(service, target):
    matches = [item for item in services[service].get("volumes", []) if item.get("target") == target]
    assert len(matches) == 1, (service, target, matches)
    return matches[0]


assert not mount("operator-auth-init", expected_secret_mount).get("read_only", False)
for name in ("researchd", "ui"):
    assert mount(name, expected_secret_mount).get("read_only") is True, name
for name, service in services.items():
    targets = {item.get("target") for item in service.get("volumes", [])}
    if name not in ("operator-auth-init", "researchd", "ui"):
        assert expected_secret_mount not in targets, name

for name in ("researchd", "ui", "kimi-web", "kimi-token-relay"):
    service = services[name]
    assert service.get("user", "65532:65532") == "65532:65532", name
    assert "ALL" in service.get("cap_drop", []), name
    assert not service.get("cap_add"), name

healthcheck = services["researchd"]["healthcheck"]["test"]
healthcheck = " ".join(healthcheck) if isinstance(healthcheck, list) else healthcheck
assert "http://localhost:8080/healthz" in healthcheck
assert "/api/v1/" not in healthcheck

ui = services["ui"]
assert ui["environment"].get("AUTORESEARCH_UI_ALLOWED_HOSTS") == "localhost:8081,127.0.0.1:8081"
assert ui["environment"].get("AUTORESEARCH_UI_ALLOWED_ORIGINS") == "http://127.0.0.1:8081"
assert ui["environment"].get("AUTORESEARCH_UI_SESSION_MAX_AGE") == "28800"
custom_ui_environment = custom_configuration["services"]["ui"]["environment"]
assert custom_ui_environment.get("AUTORESEARCH_UI_ALLOWED_HOSTS") == "localhost:8081,127.0.0.1:18081"
assert custom_ui_environment.get("AUTORESEARCH_UI_ALLOWED_ORIGINS") == "http://127.0.0.1:18081"
assert custom_ui_environment.get("AUTORESEARCH_UI_SESSION_MAX_AGE") == "3600"
ui_healthcheck = ui["healthcheck"]["test"]
ui_healthcheck = " ".join(ui_healthcheck) if isinstance(ui_healthcheck, list) else ui_healthcheck
assert "Host: localhost:8081" in ui_healthcheck
assert "http://localhost:8081/health" in ui_healthcheck
assert ui["environment"].get("NICEGUI_STORAGE_PATH") == "/tmp/nicegui"
ui_tmpfs = ui.get("tmpfs", [])
assert any(
    entry.startswith("/tmp:")
    and "uid=65532" in entry
    and "gid=65532" in entry
    and "mode=0700" in entry
    for entry in ui_tmpfs
), ui_tmpfs

for name, service in services.items():
    for port in service.get("ports", []):
        assert port.get("host_ip") == "127.0.0.1", (name, port)

expected_kimi_mounts = {
    "/tmp/kimi-code": {"kimi-web-init", "kimi-web", "kimi-token-relay"},
    "/run/kimi-web": {"kimi-web-init", "kimi-token-relay", "researchd"},
    "/srv/autoresearch-private/kimi-sessions": {"kimi-web-init", "kimi-web", "researchd"},
}
for target, expected in expected_kimi_mounts.items():
    actual = {
        name
        for name, service in services.items()
        if any(item.get("target") == target for item in service.get("volumes", []))
    }
    assert actual == expected, (target, actual)
assert "KIMI_WEB_TOKEN_FILE" not in ui.get("environment", {})
PY
printf 'ok - Compose isolates the operator secret and preserves service security boundaries\n'
