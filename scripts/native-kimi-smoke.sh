#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
source scripts/lib/operator-auth.sh
export COMPOSE_PROJECT_NAME=autoresearch-native-kimi-smoke
export KIMI_WEB_PORT=18627
export AUTORESEARCH_CONFIG=configs/kimi-web-demo.yaml
daemon=${COMPOSE_PROJECT_NAME}-daemon
compose=(docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web)
cleanup() {
  docker rm -f "$daemon" >/dev/null 2>&1 || true
  "${compose[@]}" down --volumes >/dev/null 2>&1 || true
  docker volume rm "${COMPOSE_PROJECT_NAME}_kimi-code-home" >/dev/null 2>&1 || true
  if [ "${1:-}" != "--preserve-operator-auth" ]; then
    autoresearch_cleanup_operator_auth
  fi
}
trap cleanup EXIT INT TERM
autoresearch_prepare_operator_auth
cleanup --preserve-operator-auth
"${compose[@]}" config >/dev/null
"${compose[@]}" run --rm --no-deps operator-auth-init
docker volume create   --label "com.docker.compose.project=${COMPOSE_PROJECT_NAME}"   --label "com.docker.compose.volume=kimi-code-home"   "${COMPOSE_PROJECT_NAME}_kimi-code-home" >/dev/null
docker run --rm --user 0:0 -v "${COMPOSE_PROJECT_NAME}_kimi-code-home:/tmp/kimi-code" --entrypoint sh   node@sha256:7c400d78797d8cbe3f6bb21d2227b86af4cd24b0014cc9f62ff989cc16de988e   -c 'mkdir -m 0700 -p /tmp/kimi-code/legacy && printf legacy-marker > /tmp/kimi-code/legacy/private-marker && chmod 0600 /tmp/kimi-code/legacy/private-marker && printf legacy-native-token > /tmp/kimi-code/server.token && chmod 0644 /tmp/kimi-code/server.token'
"${compose[@]}" up -d --build kimi-token-relay
for _ in $(seq 1 120); do
  if curl -fsS http://127.0.0.1:${KIMI_WEB_PORT}/api/v1/healthz >/dev/null 2>&1 && "${compose[@]}" exec -T kimi-token-relay test -r /run/kimi-web/server.token >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS http://127.0.0.1:${KIMI_WEB_PORT}/api/v1/healthz >/dev/null
"${compose[@]}" exec -T kimi-web sh -c 'test "$(id -u)" = 65532'
if ! "${compose[@]}" exec -T kimi-web test -r /tmp/kimi-code/legacy/private-marker; then
  echo 'Legacy Kimi home files are not readable by the non-root service' >&2
  exit 1
fi
"${compose[@]}" exec -T kimi-token-relay sh -c 'test "$(stat -c %a /run/kimi-web/server.token)" = 400'
"${compose[@]}" build researchd
"${compose[@]}" run --rm --no-deps researchd ./bin/researchctl validate --config configs/kimi-web-demo.yaml >/dev/null
"${compose[@]}" run -d --name "$daemon" --no-deps researchd >/dev/null
for _ in $(seq 1 60); do
  if docker exec "$daemon" ./bin/researchctl status --api http://localhost:8080 >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$daemon" ./bin/researchctl status --api http://localhost:8080 >/dev/null
unauthenticated_status=$(docker exec "$daemon" curl -sS -o /dev/null -w '%{http_code}' http://localhost:8080/api/v1/run/status)
if [ "$unauthenticated_status" != 401 ]; then
  printf 'Unauthenticated API status was %s; expected 401\n' "$unauthenticated_status" >&2
  exit 1
fi
docker exec "$daemon" ./bin/researchctl agents --api http://localhost:8080 | python3 -c 'import json, sys; agents=json.load(sys.stdin)["agents"]; assert len(agents)==6; assert sum(a["provider"]=="kimi-web" for a in agents)==5'
docker run --rm \
  --network "${COMPOSE_PROJECT_NAME}_default" \
  --user 65532:65532 \
  -e HOME=/tmp \
  -e GOCACHE=/tmp/autoresearch-go-cache \
  -e GOMODCACHE=/tmp/autoresearch-go-mod \
  -e AUTORESEARCH_KIMI_WEB_URL=http://kimi-web:58627 \
  -e AUTORESEARCH_KIMI_WEB_TOKEN_FILE=/run/kimi-web/server.token \
  -e AUTORESEARCH_KIMI_WEB_WORKSPACE=/srv/autoresearch-private/kimi-sessions \
  -v "$PWD:/src:ro" \
  -v "${COMPOSE_PROJECT_NAME}_kimi-web-token:/run/kimi-web:ro" \
  -v "${COMPOSE_PROJECT_NAME}_kimi-sessions:/srv/autoresearch-private/kimi-sessions" \
  -w /src/tests \
  golang@sha256:1a6d4452c65dea36aac2e2d606b01b4a029ec90cc1ae53890540ce6173ea77ac \
  go test -race -tags=nativekimi -v -timeout 60s .
printf '%s\n' 'Native Kimi daemon, sessions, and private token handoff verified; no research run or model prompt was submitted.'
