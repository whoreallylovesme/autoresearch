#!/bin/sh
set -eu
source="${KIMI_CODE_HOME:?}/server.token"
directory="${AUTORESEARCH_KIMI_TOKEN_DIR:?}"
destination="$directory/server.token"
umask 077
trap 'exit 0' INT TERM
while true; do
  if [ -r "$source" ]; then
    temporary="$directory/.server.token.$$"
    if cp -- "$source" "$temporary"; then
      chmod 0400 "$temporary"
      if [ ! -r "$destination" ] || ! cmp -s -- "$temporary" "$destination"; then
        mv -- "$temporary" "$destination"
      else
        rm -f -- "$temporary"
      fi
    else
      rm -f -- "$temporary"
    fi
  fi
  sleep 1
done
