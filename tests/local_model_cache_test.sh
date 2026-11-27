#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

model_name='qwen2.5-0.5b-instruct-q4_k_m.gguf'
model_hash='74a4da8c9fdbcd15bd1f6d01d621410d31c6fc00986f5eb687824e7b93d7a9db'
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
stub_dir="$test_root/bin"
mkdir -p "$stub_dir"

cat > "$stub_dir/curl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CURL_LOG"
exit 97
STUB

cat > "$stub_dir/docker" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$DOCKER_LOG"
case " $* " in
  *" config "*) exit 42 ;;
esac
exit 99
STUB

cat > "$stub_dir/sha256sum" <<'STUB'
#!/usr/bin/env bash
IFS= read -r checksum_line
expected=${checksum_line%% *}
model_file=${checksum_line#*  }
printf '%s\n' "$checksum_line" > "$SHA256_LOG"
if [ "$expected" != "$EXPECTED_MODEL_SHA256" ]; then
  printf 'unexpected model checksum: %s\n' "$expected" >&2
  exit 98
fi
if [ "${STUB_MODEL_VALID:-0}" = '1' ]; then
  printf '%s: OK\n' "$model_file"
  exit 0
fi
printf '%s\n' "$checksum_line" | "$REAL_SHA256SUM" -c -
STUB
chmod +x "$stub_dir/curl" "$stub_dir/docker" "$stub_dir/sha256sum"

export AUTORESEARCH_OPERATOR_TOKEN_FILE="$test_root/operator.token"
go run ./cmd/researchctl auth generate --file "$AUTORESEARCH_OPERATOR_TOKEN_FILE" >/dev/null

cache_dir="$test_root/valid-cache"
mkdir -p "$cache_dir"
printf 'cached model\n' > "$cache_dir/$model_name"
output=$(env \
  PATH="$stub_dir:$PATH" \
  AUTORESEARCH_MODEL_DIR="$cache_dir" \
  CURL_LOG="$test_root/valid-curl.log" \
  SHA256_LOG="$test_root/valid-sha256.log" \
  EXPECTED_MODEL_SHA256="$model_hash" \
  STUB_MODEL_VALID=1 \
  bash -c 'source scripts/lib/local-model.sh
autoresearch_prepare_model
printf "%s\n%s\n" "$model_dir" "$model_file"')
expected_output=$(printf '%s: OK\n%s\n%s' "$cache_dir/$model_name" "$cache_dir" "$cache_dir/$model_name")
if [ "$output" != "$expected_output" ]; then
  printf 'valid cache returned unexpected output:\n%s\n' "$output" >&2
  exit 1
fi
if [ -e "$test_root/valid-curl.log" ]; then
  printf 'valid cache unexpectedly invoked curl\n' >&2
  exit 1
fi
if [ "$(cat "$test_root/valid-sha256.log")" != "$model_hash  $cache_dir/$model_name" ]; then
  printf 'valid cache checked the wrong model or checksum\n' >&2
  exit 1
fi
printf 'ok - valid cache avoids download and preserves checksum output\n'

cache_dir="$test_root/corrupt-cache"
mkdir -p "$cache_dir"
printf 'corrupt model\n' > "$cache_dir/$model_name"
if env \
  PATH="$stub_dir:$PATH" \
  AUTORESEARCH_MODEL_DIR="$cache_dir" \
  CURL_LOG="$test_root/corrupt-curl.log" \
  DOCKER_LOG="$test_root/corrupt-docker.log" \
  SHA256_LOG="$test_root/corrupt-sha256.log" \
  EXPECTED_MODEL_SHA256="$model_hash" \
  REAL_SHA256SUM="$(command -v sha256sum)" \
  STUB_MODEL_VALID=0 \
  bash scripts/local-model-demo.sh > "$test_root/corrupt-output.log" 2>&1; then
  printf 'corrupt cache unexpectedly succeeded\n' >&2
  exit 1
fi
if [ -e "$test_root/corrupt-curl.log" ] || [ -e "$test_root/corrupt-docker.log" ]; then
  printf 'corrupt cache reached download or container startup\n' >&2
  exit 1
fi
if [ "$(cat "$test_root/corrupt-sha256.log")" != "$model_hash  $cache_dir/$model_name" ]; then
  printf 'corrupt cache checked the wrong model or checksum\n' >&2
  exit 1
fi
printf 'ok - corrupt cache fails before download or container startup\n'

cache_dir="$test_root/custom models/nested"
mkdir -p "$cache_dir"
printf 'cached model\n' > "$cache_dir/$model_name"
status=0
env \
  PATH="$stub_dir:$PATH" \
  AUTORESEARCH_MODEL_DIR="$cache_dir" \
  CURL_LOG="$test_root/custom-curl.log" \
  DOCKER_LOG="$test_root/custom-docker.log" \
  SHA256_LOG="$test_root/custom-sha256.log" \
  EXPECTED_MODEL_SHA256="$model_hash" \
  STUB_MODEL_VALID=1 \
  bash scripts/compose-smoke.sh > "$test_root/custom-output.log" 2>&1 || status=$?
if [ "$status" -ne 42 ]; then
  printf 'custom model directory check did not reach the stubbed Compose config (status %s)\n' "$status" >&2
  cat "$test_root/custom-output.log" >&2
  exit 1
fi
if [ -e "$test_root/custom-curl.log" ]; then
  printf 'custom model directory unexpectedly invoked curl\n' >&2
  exit 1
fi
if [ "$(cat "$test_root/custom-sha256.log")" != "$model_hash  $cache_dir/$model_name" ]; then
  printf 'custom model directory checked the wrong model or checksum\n' >&2
  exit 1
fi
while IFS= read -r invocation; do
  case "$invocation" in
    *" up "*)
      printf 'custom model directory check started Compose services\n' >&2
      exit 1
      ;;
  esac
done < "$test_root/custom-docker.log"
printf 'ok - custom model directory avoids unnecessary downloads\n'
