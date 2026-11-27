#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/operator-auth.sh
config_path=${1:-configs/experiment.yaml}
mkdir -p bin
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
uv sync --frozen --project web
data_dir=$(mktemp -d)
mkdir -m 0700 "$data_dir/.nicegui"
export NICEGUI_STORAGE_PATH="$data_dir/.nicegui"
cleanup() {
  kill "${daemon_pid:-}" "${ui_pid:-}" 2>/dev/null || true
  wait "${daemon_pid:-}" "${ui_pid:-}" 2>/dev/null || true
  rm -rf "$data_dir"
  autoresearch_cleanup_operator_auth
}
trap cleanup EXIT INT TERM
autoresearch_prepare_operator_auth
./bin/researchd --config "$config_path" --data-dir "$data_dir" &
daemon_pid=$!
(
  cd web
  uv run --frozen --project . python -m autoresearch_ui.main
) &
ui_pid=$!
for _ in $(seq 1 100); do
  if curl -fsS http://127.0.0.1:8081/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
printf 'UI login: http://127.0.0.1:8081 (operator token file: %s)\n' "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
./bin/researchctl run --config "$config_path" --api http://127.0.0.1:8080 --timeout 90s
wait -n
