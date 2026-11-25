GO ?= go

.PHONY: fmt fmt-check lint test test-integration run-demo validate

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
