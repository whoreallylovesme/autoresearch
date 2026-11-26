#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export COMPOSE_PROJECT_NAME=autoresearch-smoke
export AUTORESEARCH_API_PORT=18080
export AUTORESEARCH_UI_PORT=18081
export AUTORESEARCH_MODEL_PORT=18082
cleanup() {
  docker compose -f deploy/docker-compose.yml down --volumes >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
model_dir=${AUTORESEARCH_MODEL_DIR:-/opt/models}
model_file="$model_dir/qwen2.5-0.5b-instruct-q4_k_m.gguf"
mkdir -p "$model_dir"
if [ ! -f "$model_file" ]; then
  curl -fL --retry 3 --retry-delay 2 https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf -o "$model_file"
fi
printf '%s  %s\n' '74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db' "$model_file" | sha256sum -c -
docker compose -f deploy/docker-compose.yml config >/dev/null
docker compose -f deploy/docker-compose.yml up -d --build
if docker compose -f deploy/docker-compose.yml ps --status running --services | grep -Eq '^kimi-(web|token-relay)$'; then
  echo 'Native Kimi services must not start in the default Compose profile' >&2
  exit 1
fi
for _ in $(seq 1 120); do
  if curl -fsS http://127.0.0.1:${AUTORESEARCH_UI_PORT}/health >/dev/null 2>&1 && curl -fsS http://127.0.0.1:${AUTORESEARCH_API_PORT}/api/v1/run/status >/dev/null 2>&1 && curl -fsS http://127.0.0.1:${AUTORESEARCH_MODEL_PORT}/health >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker compose -f deploy/docker-compose.yml exec -T researchd ./bin/researchctl run --config configs/local-hf.yaml --api http://localhost:8080 --timeout 90s
curl -fsS http://127.0.0.1:${AUTORESEARCH_UI_PORT}/health
