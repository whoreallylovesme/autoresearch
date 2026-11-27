GO ?= go
UV ?= uv

.PHONY: fmt fmt-check lint test test-integration auth-test run-demo literature-demo experiment-demo control-center compose-test browser-test full-research-demo local-model-demo web-setup web-lint web-test validate native-kimi-test

fmt:
	$(GO) fmt ./...

fmt-check:
	@test -z "$$(gofmt -l .)" || { echo "Go files need formatting"; exit 1; }

lint:
	$(GO) vet ./...

test:
	$(GO) test -race -shuffle=on ./...

test-integration:
	$(GO) test -race -tags=integration ./tests/...

auth-test:
	bash scripts/tests/operator_auth_test.sh
	bash scripts/tests/compose_auth_test.sh
	bash scripts/tests/script_auth_static_test.sh
	@set -e; \
	token_dir=$$(mktemp -d); \
	trap 'rm -rf "$$token_dir"' EXIT; \
	printf '%s\n' 'local-model-cache-test-only-token' > "$$token_dir/operator.token"; \
	chmod 0600 "$$token_dir/operator.token"; \
	AUTORESEARCH_OPERATOR_TOKEN_FILE="$$token_dir/operator.token" bash tests/local_model_cache_test.sh

validate:
	$(GO) run ./cmd/researchctl validate --config configs/autoresearch.yaml

run-demo:
	bash scripts/demo.sh

literature-demo:
	bash scripts/demo.sh configs/literature.yaml

experiment-demo:
	bash scripts/demo.sh configs/experiment.yaml

web-setup:
	cd web && $(UV) sync --frozen

web-lint: web-setup
	cd web && $(UV) run --frozen ruff check . && $(UV) run --frozen ruff format --check .

web-test: web-setup
	cd web && $(UV) run --frozen pytest -q

control-center:
	bash scripts/control-center.sh

browser-test: web-setup
	bash scripts/browser-smoke.sh

local-model-demo:
	bash scripts/local-model-demo.sh

full-research-demo:
	bash scripts/demo.sh configs/full-research.yaml

compose-test:
	bash scripts/compose-smoke.sh

native-kimi-test:
	bash scripts/native-kimi-smoke.sh
