GO ?= go
SCRIPTS := $(shell find scenarios images -name '*.sh' 2>/dev/null)

.PHONY: build test vet fmt shellcheck validate check

build:
	$(GO) build -o bin/opsschool ./cmd/opsschool

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

shellcheck:
	@if command -v shellcheck >/dev/null; then shellcheck -x $(SCRIPTS); else echo "shellcheck not installed; skipping"; fi

validate:
	$(GO) run ./cmd/opsschool validate scenarios

check: fmt vet test shellcheck validate
