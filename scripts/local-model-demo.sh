#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/operator-auth.sh
source scripts/lib/local-model.sh
image='ghcr.io/ggml-org/llama.cpp@sha256:b0cd5490eefff30b40dcd47f7249a7d7211136908f73fe440a2a86b35a22b23b'
started_container=''
cleanup() {
  if [ -n "$started_container" ]; then
    docker stop "$started_container" >/dev/null 2>&1 || true
  fi
  autoresearch_cleanup_operator_auth
}
trap cleanup EXIT INT TERM
autoresearch_prepare_operator_auth
autoresearch_prepare_model
if ! curl -fsS http://127.0.0.1:8082/health >/dev/null 2>&1; then
  docker pull "$image" >/dev/null
  started_container=$(docker run -d --rm --name autoresearch-llama-demo -p 127.0.0.1:8082:8080 -v "$model_dir:/models:ro" "$image" -m /models/qwen2.5-0.5b-instruct-q4_k_m.gguf --host 0.0.0.0 --port 8080 --threads 4 --ctx-size 8192 --parallel 2)
fi
for _ in $(seq 1 120); do
  if curl -fsS http://127.0.0.1:8082/health >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
go test -race -tags=localmodel -timeout 120s ./tests
bash scripts/demo.sh configs/local-hf.yaml
