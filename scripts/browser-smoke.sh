#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/operator-auth.sh
npm ci --prefix web
if [ "${AUTORESEARCH_PLAYWRIGHT_SKIP_INSTALL:-0}" != "1" ]; then
  npx --prefix web playwright install --with-deps chromium
fi
mkdir -p bin
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
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
./bin/researchd --config configs/experiment.yaml --listen 127.0.0.1:18080 --data-dir "$data_dir" &
daemon_pid=$!
(
  cd web
  AUTORESEARCH_API=http://127.0.0.1:18080/api/v1 AUTORESEARCH_UI_PORT=18081 uv run --frozen --project . python -m autoresearch_ui.main
) &
ui_pid=$!
for _ in $(seq 1 100); do
  if curl -fsS http://127.0.0.1:18081/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
./bin/researchctl run --config configs/experiment.yaml --api http://127.0.0.1:18080 --timeout 90s >/dev/null
AUTORESEARCH_API=http://127.0.0.1:18080/api/v1 AUTORESEARCH_UI_URL=http://127.0.0.1:18081 npm --prefix web run test:browser
