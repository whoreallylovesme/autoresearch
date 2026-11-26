#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export COMPOSE_PROJECT_NAME=autoresearch-native-kimi-smoke
export KIMI_WEB_PORT=18627
export AUTORESEARCH_CONFIG=configs/kimi-web-demo.yaml
daemon=${COMPOSE_PROJECT_NAME}-daemon
compose=(docker compose -f deploy/docker-compose.yml -f deploy/kimi-web.compose.yml --profile kimi-web)
cleanup() {
  docker rm -f "$daemon" >/dev/null 2>&1 || true
  "${compose[@]}" down --volumes >/dev/null 2>&1 || true
  docker volume rm "${COMPOSE_PROJECT_NAME}_kimi-code-home" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM
cleanup
"${compose[@]}" config >/dev/null
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
"${compose[@]}" run --rm --no-deps researchd ./bin/researchctl validate --config configs/kimi-web-demo.yaml >/dev/null
"${compose[@]}" run -d --name "$daemon" --no-deps researchd >/dev/null
for _ in $(seq 1 60); do
  if docker exec "$daemon" curl -fsS http://localhost:8080/api/v1/run/status >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$daemon" curl -fsS http://localhost:8080/api/v1/run/status >/dev/null
docker exec "$daemon" python -c 'import json, urllib.request; agents=json.load(urllib.request.urlopen("http://localhost:8080/api/v1/agents"))["agents"]; assert len(agents)==6; assert sum(a["provider"]=="kimi-web" for a in agents)==5'
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
