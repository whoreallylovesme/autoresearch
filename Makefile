GO ?= go
UV ?= uv

.PHONY: fmt fmt-check lint test test-integration run-demo literature-demo experiment-demo control-center compose-test browser-test full-research-demo local-model-demo web-setup web-lint web-test validate

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
