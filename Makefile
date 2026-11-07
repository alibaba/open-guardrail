GO ?= go
NODE ?= node
PYTHON ?= python3
VERSION ?= dev
FUZZ_TIME ?= 10s
FUZZ_PARALLEL ?= 2
export GOWORK := off
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/alibaba/open-guardrail/internal/buildinfo.Version=$(VERSION) -X github.com/alibaba/open-guardrail/internal/buildinfo.Commit=$(COMMIT)

.PHONY: build check-dependency-licenses check-dependency-notices check-generated check-public-release generate test test-go test-fuzz test-web test-python test-scripts demo smoke smoke-docker smoke-release preview-release release public-release
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/open-guardrail ./cmd/open-guardrail
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/guardrailctl ./cmd/guardrailctl

generate:
	$(GO) generate ./api/openapi

check-generated:
	GO="$(GO)" sh scripts/check-generated.sh

check-dependency-licenses:
	sh scripts/check-dependency-licenses.sh

check-dependency-notices:
	$(PYTHON) scripts/dependency-notices.py

test: test-go test-web test-python test-scripts

test-go: check-generated
	sh scripts/check-toolchain.sh
	test -z "$$(gofmt -l cmd internal api migrations storage web)"
	$(GO) vet ./...
	$(GO) test -race ./...

test-fuzz:
	$(GO) test ./internal/domain/spec -run '^$$' -fuzz '^FuzzValidateSpec$$' -fuzztime "$(FUZZ_TIME)" -fuzzminimizetime=5s -parallel "$(FUZZ_PARALLEL)" -timeout=2m
	$(GO) test ./internal/domain/spec -run '^$$' -fuzz '^FuzzSpecTextIdentity$$' -fuzztime "$(FUZZ_TIME)" -fuzzminimizetime=5s -parallel "$(FUZZ_PARALLEL)" -timeout=2m
	$(GO) test ./internal/domain/run -run '^$$' -fuzz '^FuzzCanonicalizeChangeSet$$' -fuzztime "$(FUZZ_TIME)" -fuzzminimizetime=5s -parallel "$(FUZZ_PARALLEL)" -timeout=2m
	$(GO) test ./internal/domain/run -run '^$$' -fuzz '^FuzzRejectDuplicateChangeSetKeys$$' -fuzztime "$(FUZZ_TIME)" -fuzzminimizetime=5s -parallel "$(FUZZ_PARALLEL)" -timeout=2m

test-web:
	$(NODE) --test web/*.test.cjs scripts/spec-canonicalization.test.cjs

test-python:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s integrations -t . -p 'test_*.py'
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s examples/agent-demo -p 'test_*.py'
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s examples/cleanup-demo -p 'test_*.py'

test-scripts:
	sh scripts/check-generated.test.sh
	sh scripts/package.test.sh
	sh scripts/check-public-release.test.sh
	sh scripts/check-dependency-licenses.test.sh
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) scripts/dependency-notices.test.py
	$(MAKE) check-dependency-licenses
	$(MAKE) check-dependency-notices

smoke:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) examples/agent-demo/smoke.py

demo:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) examples/cleanup-demo/demo.py

smoke-docker:
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) scripts/smoke_docker.py

preview-release:
	PACKAGE_KIND=preview VERSION=$(VERSION) GO="$(GO)" PYTHON="$(PYTHON)" sh scripts/package.sh

check-public-release:
	VERSION=$(VERSION) PYTHON="$(PYTHON)" sh scripts/check-public-release.sh

release: public-release

public-release:
	$(MAKE) check-public-release
	$(MAKE) check-dependency-licenses
	$(MAKE) check-dependency-notices
	PACKAGE_KIND=public VERSION=$(VERSION) GO="$(GO)" PYTHON="$(PYTHON)" sh scripts/package.sh

# Verify preview artifacts without bypassing the separate public-release gates.
smoke-release: preview-release
	$(PYTHON) scripts/smoke_release.py --version "$(VERSION)"
