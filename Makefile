GO ?= go

.PHONY: fmt fmt-check lint test test-integration run-demo literature-demo experiment-demo control-center browser-test web-setup web-lint web-test validate

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
	python3 -m venv .venv
	./.venv/bin/python -m pip install -q -r web/requirements.txt -r web/requirements-dev.txt

web-lint: web-setup
	cd web && ../.venv/bin/ruff check . && ../.venv/bin/ruff format --check .

web-test: web-setup
	cd web && ../.venv/bin/pytest -q

control-center:
	bash scripts/control-center.sh

browser-test: web-setup
	bash scripts/browser-smoke.sh
