#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
config_path=${1:-configs/autoresearch.yaml}
mkdir -p bin
go build -o bin/researchd ./cmd/researchd
go build -o bin/researchctl ./cmd/researchctl
data_dir=$(mktemp -d)
address=$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{sock.getsockname()[1]}")
PY
)
./bin/researchd --config "$config_path" --listen "$address" --data-dir "$data_dir" &
daemon_pid=$!
cleanup() {
  kill "$daemon_pid" 2>/dev/null || true
  wait "$daemon_pid" 2>/dev/null || true
  rm -rf "$data_dir"
}
trap cleanup EXIT INT TERM
for _ in $(seq 1 100); do
  if ./bin/researchctl status --api "http://$address" >/dev/null 2>&1; then
    break
  fi
  sleep 0.05
done
./bin/researchctl run --config "$config_path" --api "http://$address" --timeout 30s
