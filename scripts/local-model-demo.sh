#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
model_dir=${AUTORESEARCH_MODEL_DIR:-/opt/models}
model_file="$model_dir/qwen2.5-0.5b-instruct-q4_k_m.gguf"
image='ghcr.io/ggml-org/llama.cpp@sha256:b0cd5490eefff30b40dcd47f7249a7d7211136908f73fe440a2a86b35a22b23b'
mkdir -p "$model_dir"
if [ ! -f "$model_file" ]; then
  curl -fL --retry 3 --retry-delay 2 https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf -o "$model_file"
fi
printf '%s  %s\n' '74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db' "$model_file" | sha256sum -c -
started_container=''
cleanup() {
  if [ -n "$started_container" ]; then
    docker stop "$started_container" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM
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
