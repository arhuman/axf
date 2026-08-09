# axf: reference Go SDK, runtime and CLI for AXF (Alter eXtensible Format)

BINARY     := axf
CMD        := ./cmd/axf
BIN_DIR    := bin
SRC_DIRS   := . conformance cmd internal runtime
COVER_MIN  := 80
COVER_OUT  := coverage.out
COVER_HTML := coverage.html

# Pinned dev-tool versions installed by `make tools` (reproducible audits).
GOLANGCI_VERSION    := v2.12.2
GOVULNCHECK_VERSION := v1.1.4

# Version metadata injected into internal/version via -ldflags -X. VERSION is a
# git describe (tag-or-commit), falling back to "dev" outside a git checkout.
# BUILD_DATE follows the commit timestamp so rebuilds stay reproducible.
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell git show -s --format=%cI HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
VPKG       := github.com/arhuman/axf/internal/version
LDFLAGS    := -ldflags "-X $(VPKG).Version=$(VERSION) -X $(VPKG).GitCommit=$(COMMIT) -X $(VPKG).BuildDate=$(BUILD_DATE)"

.PHONY: audit build clean cover help install test tidy tools

# First target is the default (`make` == `make build`).
## build: compile the axf binary (cgo-free) into bin/
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD)

## audit: full quality gate (lint, mod verify, vuln scan, race+coverage gate)
audit:
	go mod verify
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	$(MAKE) cover

## clean: remove build artifacts
clean:
	rm -rf $(BIN_DIR) $(COVER_OUT) $(COVER_HTML)

## cover: run tests with coverage, write reports, fail under COVER_MIN% total
cover:
	go test -race -covermode=atomic -coverprofile=$(COVER_OUT) ./...
	go tool cover -func=$(COVER_OUT)
	go tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@total=$$(go tool cover -func=$(COVER_OUT) | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	echo "total coverage: $$total% (minimum $(COVER_MIN)%)"; \
	awk "BEGIN{ exit !($$total+0 >= $(COVER_MIN)) }" || \
	  { echo "FAIL: total coverage $$total% is below the $(COVER_MIN)% gate"; exit 1; }

## install: install the axf binary into GOBIN (cgo-free)
install:
	CGO_ENABLED=0 go install $(LDFLAGS) $(CMD)

## test: run all tests with the race detector
test:
	go test -race ./...

## tidy: tidy go.mod and gofmt the source tree
tidy:
	go mod tidy
	gofmt -w $(SRC_DIRS)

## tools: install pinned Go dev tools into GOBIN
tools:
	@echo "Installing Go tools..."
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Tools installed in $(shell go env GOBIN || go env GOPATH)/bin"

## help: list available targets
help:
	@grep -E '^## [a-z-]+:' $(MAKEFILE_LIST) | sed -E 's/^## //' | sort

.DEFAULT_GOAL := build
