#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

helper="$PWD/scripts/lib/operator-auth.sh"
if [ ! -f "$helper" ]; then
  printf 'operator authentication helper is missing\n' >&2
  exit 1
fi

test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT
marker='shell-test-only-operator-token'
stub_dir="$test_root/stubs"
mkdir -p "$stub_dir"

write_generator() {
  cat > "$1" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$GENERATOR_LOG"
target=''
while [ "$#" -gt 0 ]; do
  if [ "$1" = '--file' ]; then
    target=$2
    shift 2
  else
    shift
  fi
done
if [ -z "$target" ]; then
  exit 91
fi
umask 077
printf '%s\n' 'shell-test-only-operator-token' > "$target"
printf 'generator emitted shell-test-only-operator-token\n'
printf 'generator error emitted shell-test-only-operator-token\n' >&2
STUB
  chmod +x "$1"
}

write_generator "$stub_dir/go"

existing_dir="$test_root/existing"
mkdir -p "$existing_dir/bin"
write_generator "$existing_dir/bin/researchctl"
existing_file="$existing_dir/operator.token"
printf '%s\n' "$marker" > "$existing_file"
chmod 0600 "$existing_file"
output=$(
  cd "$existing_dir"
  env \
    OPERATOR_AUTH_HELPER="$helper" \
    GENERATOR_LOG="$existing_dir/generator.log" \
    AUTORESEARCH_OPERATOR_TOKEN_FILE="$existing_file" \
    AUTORESEARCH_OPERATOR_TOKEN='must-not-remain-exported' \
    bash -c '
      set -euo pipefail
      source "$OPERATOR_AUTH_HELPER"
      autoresearch_prepare_operator_auth
      test -z "${AUTORESEARCH_OPERATOR_TOKEN:-}"
      autoresearch_cleanup_operator_auth
      printf "%s\n" "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
    ' 2>&1
)
if [ "$output" != "$existing_file" ]; then
  printf 'existing token file was not preserved: %s\n' "$output" >&2
  exit 1
fi
if [ -e "$existing_dir/generator.log" ] || [ ! -f "$existing_file" ]; then
  printf 'existing token file invoked the generator or was cleaned up\n' >&2
  exit 1
fi
printf 'ok - existing token file is reused without generation or cleanup\n'

missing_dir="$test_root/missing"
mkdir -p "$missing_dir/bin"
write_generator "$missing_dir/bin/researchctl"
if (
  cd "$missing_dir"
  env \
    OPERATOR_AUTH_HELPER="$helper" \
    GENERATOR_LOG="$missing_dir/generator.log" \
    AUTORESEARCH_OPERATOR_TOKEN_FILE="$missing_dir/absent.token" \
    bash -c 'source "$OPERATOR_AUTH_HELPER"; autoresearch_prepare_operator_auth'
) >"$missing_dir/output.log" 2>&1; then
  printf 'missing configured token file unexpectedly succeeded\n' >&2
  exit 1
fi
if [ -e "$missing_dir/generator.log" ]; then
  printf 'missing configured token file unexpectedly invoked the generator\n' >&2
  exit 1
fi
printf 'ok - missing configured token file fails without generating a replacement\n'

built_dir="$test_root/built"
mkdir -p "$built_dir/bin"
write_generator "$built_dir/bin/researchctl"
output=$(
  cd "$built_dir"
  env \
    PATH="$stub_dir:$PATH" \
    OPERATOR_AUTH_HELPER="$helper" \
    TMPDIR="$built_dir" \
    GENERATOR_LOG="$built_dir/generator.log" \
    AUTORESEARCH_OPERATOR_TOKEN='must-not-remain-exported' \
    AUTORESEARCH_OPERATOR_AUTH_CLI=./bin/researchctl \
    bash -c '
      set -euo pipefail
      source "$OPERATOR_AUTH_HELPER"
      autoresearch_prepare_operator_auth
      test -z "${AUTORESEARCH_OPERATOR_TOKEN:-}"
      test -f "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
      test "$(stat -c %a "$AUTORESEARCH_OPERATOR_TOKEN_FILE")" = 600
      autoresearch_cleanup_operator_auth
      test ! -e "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
      printf "%s\n" "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
    ' 2>&1
)
if [[ "$output" == *"$marker"* ]]; then
  printf 'generated token appeared in helper output\n' >&2
  exit 1
fi
case "$output" in
  "$built_dir/autoresearch-operator."*"/operator.token") ;;
  *) printf 'built generator returned an unexpected token path\n' >&2; exit 1 ;;
esac
if [ ! -f "$built_dir/generator.log" ]; then
  printf 'built researchctl generator was not invoked\n' >&2
  exit 1
fi
case "$(cat "$built_dir/generator.log")" in
  "auth generate --file "*) ;;
  *) printf 'built researchctl received unexpected arguments\n' >&2; exit 1 ;;
esac
if [ -e "$stub_dir/generator.log" ]; then
  printf 'built researchctl was available but go run was invoked\n' >&2
  exit 1
fi
if [ -e "${output%/*}" ]; then
  printf 'generated token directory survived cleanup\n' >&2
  exit 1
fi
printf 'ok - explicit researchctl override generates an owner-only ephemeral file without logging its token\n'

fallback_dir="$test_root/fallback"
mkdir -p "$fallback_dir"
output=$(
  cd "$fallback_dir"
  env \
    PATH="$stub_dir:$PATH" \
    OPERATOR_AUTH_HELPER="$helper" \
    TMPDIR="$fallback_dir" \
    GENERATOR_LOG="$fallback_dir/generator.log" \
    bash -c '
      set -euo pipefail
      source "$OPERATOR_AUTH_HELPER"
      autoresearch_prepare_operator_auth
      test -f "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
      test "$(stat -c %a "$AUTORESEARCH_OPERATOR_TOKEN_FILE")" = 600
      autoresearch_cleanup_operator_auth
      printf "%s\n" "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
    ' 2>&1
)
if [[ "$output" == *"$marker"* ]]; then
  printf 'go run generator token appeared in helper output\n' >&2
  exit 1
fi
case "$(cat "$fallback_dir/generator.log")" in
  "run ./cmd/researchctl auth generate --file "*) ;;
  *) printf 'go run generator received unexpected arguments\n' >&2; exit 1 ;;
esac
if [ -e "${output%/*}" ]; then
  printf 'go run token directory survived cleanup\n' >&2
  exit 1
fi
printf 'ok - go run fallback generates and cleans an ephemeral token without logging it\n'

assert_reused_file_rejected() {
  local case_dir=$1
  shift
  if (
    cd "$case_dir"
    env \
      "$@" \
      OPERATOR_AUTH_HELPER="$helper" \
      GENERATOR_LOG="$case_dir/generator.log" \
      AUTORESEARCH_OPERATOR_TOKEN_FILE="$case_dir/operator.token" \
      bash -c 'source "$OPERATOR_AUTH_HELPER"; autoresearch_prepare_operator_auth'
  ) >"$case_dir/output.log" 2>&1; then
    printf 'insecure reused token file unexpectedly succeeded: %s\n' "$case_dir" >&2
    exit 1
  fi
  if [ -e "$case_dir/generator.log" ]; then
    printf 'insecure reused token file invoked the generator: %s\n' "$case_dir" >&2
    exit 1
  fi
}

readonly_dir="$test_root/readonly"
mkdir -p "$readonly_dir/bin"
write_generator "$readonly_dir/bin/researchctl"
readonly_file="$readonly_dir/operator.token"
printf '%s\n' "$marker" > "$readonly_file"
chmod 0400 "$readonly_file"
(
  cd "$readonly_dir"
  env \
    OPERATOR_AUTH_HELPER="$helper" \
    GENERATOR_LOG="$readonly_dir/generator.log" \
    AUTORESEARCH_OPERATOR_TOKEN_FILE="$readonly_file" \
    bash -c '
      set -euo pipefail
      source "$OPERATOR_AUTH_HELPER"
      autoresearch_prepare_operator_auth
      autoresearch_cleanup_operator_auth
    '
)
if [ -e "$readonly_dir/generator.log" ] || [ ! -f "$readonly_file" ] || [ "$(stat -c %a "$readonly_file")" != 400 ]; then
  printf '0400 token file was not preserved\n' >&2
  exit 1
fi
printf 'ok - reusable 0400 token file is accepted without modification\n'

insecure_dir="$test_root/insecure-mode"
mkdir -p "$insecure_dir/bin"
write_generator "$insecure_dir/bin/researchctl"
insecure_file="$insecure_dir/operator.token"
printf '%s\n' "$marker" > "$insecure_file"
chmod 0644 "$insecure_file"
assert_reused_file_rejected "$insecure_dir"
if [ "$(stat -c %a "$insecure_file")" != 644 ]; then
  printf 'insecure token file mode was silently changed\n' >&2
  exit 1
fi
printf 'ok - insecure reused token file mode is rejected without chmod\n'

symlink_dir="$test_root/symlink"
mkdir -p "$symlink_dir/bin"
write_generator "$symlink_dir/bin/researchctl"
symlink_target="$symlink_dir/target.token"
printf '%s\n' "$marker" > "$symlink_target"
chmod 0600 "$symlink_target"
ln -s "$symlink_target" "$symlink_dir/operator.token"
assert_reused_file_rejected "$symlink_dir"
if [ ! -L "$symlink_dir/operator.token" ]; then
  printf 'symlink token file was modified or replaced\n' >&2
  exit 1
fi
printf 'ok - symlink token file is rejected as non-regular storage\n'

wrong_owner_dir="$test_root/wrong-owner"
mkdir -p "$wrong_owner_dir/bin" "$wrong_owner_dir/stubs"
write_generator "$wrong_owner_dir/bin/researchctl"
wrong_owner_file="$wrong_owner_dir/operator.token"
printf '%s\n' "$marker" > "$wrong_owner_file"
chmod 0600 "$wrong_owner_file"
cat > "$wrong_owner_dir/stubs/stat" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = '-c' ] && [ "${2:-}" = '%u' ]; then
  printf '%s\n' "$(( $(id -u) + 1 ))"
  exit 0
fi
exec "$REAL_STAT" "$@"
STUB
chmod +x "$wrong_owner_dir/stubs/stat"
assert_reused_file_rejected "$wrong_owner_dir" \
  "PATH=$wrong_owner_dir/stubs:$PATH" \
  "REAL_STAT=$(command -v stat)"
if [ "$(stat -c %a "$wrong_owner_file")" != 600 ]; then
  printf 'wrong-owner token file was modified or replaced\n' >&2
  exit 1
fi
printf 'ok - token file owned by another UID is rejected without modification\n'
