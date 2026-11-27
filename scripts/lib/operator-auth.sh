autoresearch_operator_token_dir=''

autoresearch_prepare_operator_auth() {
  unset AUTORESEARCH_OPERATOR_TOKEN
  if [ -n "${AUTORESEARCH_OPERATOR_TOKEN_FILE:-}" ]; then
    if [ -L "$AUTORESEARCH_OPERATOR_TOKEN_FILE" ] || [ ! -f "$AUTORESEARCH_OPERATOR_TOKEN_FILE" ] || [ ! -r "$AUTORESEARCH_OPERATOR_TOKEN_FILE" ]; then
      printf 'AUTORESEARCH_OPERATOR_TOKEN_FILE must name a readable regular file\n' >&2
      return 1
    fi
    file_uid=$(stat -c %u -- "$AUTORESEARCH_OPERATOR_TOKEN_FILE") || return 1
    file_mode=$(stat -c %a -- "$AUTORESEARCH_OPERATOR_TOKEN_FILE") || return 1
    if [ "$file_uid" != "$(id -u)" ]; then
      printf 'AUTORESEARCH_OPERATOR_TOKEN_FILE must be owned by the current user\n' >&2
      return 1
    fi
    case "$file_mode" in
      400|600) ;;
      *)
        printf 'AUTORESEARCH_OPERATOR_TOKEN_FILE must have mode 0400 or 0600\n' >&2
        return 1
        ;;
    esac
    export AUTORESEARCH_OPERATOR_TOKEN_FILE
    return 0
  fi

  autoresearch_operator_token_dir=$(mktemp -d "${TMPDIR:-/tmp}/autoresearch-operator.XXXXXX")
  AUTORESEARCH_OPERATOR_TOKEN_FILE="$autoresearch_operator_token_dir/operator.token"
  export AUTORESEARCH_OPERATOR_TOKEN_FILE
  if [ -n "${AUTORESEARCH_OPERATOR_AUTH_CLI:-}" ]; then
    if [ ! -x "$AUTORESEARCH_OPERATOR_AUTH_CLI" ]; then
      printf 'AUTORESEARCH_OPERATOR_AUTH_CLI must be executable\n' >&2
      autoresearch_cleanup_operator_auth
      return 1
    fi
    if ! "$AUTORESEARCH_OPERATOR_AUTH_CLI" auth generate --file "$AUTORESEARCH_OPERATOR_TOKEN_FILE" >/dev/null 2>&1; then
      printf 'researchctl auth generate failed\n' >&2
      autoresearch_cleanup_operator_auth
      return 1
    fi
  elif ! go run ./cmd/researchctl auth generate --file "$AUTORESEARCH_OPERATOR_TOKEN_FILE" >/dev/null 2>&1; then
    printf 'go run researchctl auth generate failed\n' >&2
    autoresearch_cleanup_operator_auth
    return 1
  fi
  if [ ! -f "$AUTORESEARCH_OPERATOR_TOKEN_FILE" ]; then
    printf 'researchctl auth generate did not create the token file\n' >&2
    autoresearch_cleanup_operator_auth
    return 1
  fi
  chmod 0600 "$AUTORESEARCH_OPERATOR_TOKEN_FILE"
}

autoresearch_cleanup_operator_auth() {
  if [ -n "$autoresearch_operator_token_dir" ]; then
    rm -rf -- "$autoresearch_operator_token_dir"
    autoresearch_operator_token_dir=''
  fi
}
