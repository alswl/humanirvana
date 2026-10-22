# huma-nirvana-adapter Makefile

.PHONY: all build test lint coverage cover01 demo demo-e2e demo-tdd demo-app clean help

# Variables
MODULE := github.com/alswl/humanirvana
VERSION := 0.1.0
LDFLAGS := -X main.Version=$(VERSION)

# Go commands
GOCMD ?= go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOLINT := golangci-lint
GOCOVER := $(GOCMD) test -coverprofile=coverage.out -covermode=atomic

# Directories
TEST := tests

# Default target
all: help

# Build the adapter
build:
	$(GOBUILD) ./...

# Build for all platforms
build-all:
	GOOS=linux GOARCH=amd64 $(GOBUILD) ./...
	GOOS=darwin GOARCH=amd64 $(GOBUILD) ./...
	GOOS=windows GOARCH=amd64 $(GOBUILD) ./...

# Run all tests
test:
	$(GOTEST) ./... -v

# Run tests with coverage
coverage:
	$(GOCOVER)
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# COVER-01: statement-weighted coverage of the library packages must be >= 80%.
# The package set is every package of this module that the root library
# depends on; tests come from the library and tests/ in one atomic run.
# Examples are not part of the run.
COVER_DIR ?= bin/cover01
COVER_PKGS = $(shell $(GOCMD) list -deps -f '{{if .Module}}{{if eq .Module.Path "$(MODULE)"}}{{.ImportPath}}{{end}}{{end}}' .)

cover01:
	@mkdir -p $(COVER_DIR)
	@printf '%s\n' $(COVER_PKGS) > $(COVER_DIR)/packages.txt
	$(GOTEST) -count=1 -covermode=atomic -coverpkg=$(subst $(space),$(comma),$(COVER_PKGS)) -coverprofile=$(COVER_DIR)/cover.out . ./tests/... > $(COVER_DIR)/test.log 2>&1; status=$$?; cat $(COVER_DIR)/test.log; exit $$status
	$(GOCMD) run ./tests/unit/cover01/cmd -packages $(COVER_DIR)/packages.txt -profile $(COVER_DIR)/cover.out -summary $(COVER_DIR)/summary.json

empty :=
space := $(empty) $(empty)
comma := ,

# Run linter
lint:
	$(GOLINT) run ./...

# Run demo servers and E2E tests (cleans 8080/8081 first to avoid address in use)
demo-e2e: demo-e2e-clean-ports demo-nirvana demo-huma
	@sleep 3
	@$(MAKE) test-e2e

# Free ports 8080 and 8081 (optional, run before demo-e2e if ports in use)
demo-e2e-clean-ports:
	@-lsof -ti:8080 | xargs kill -9 2>/dev/null || true
	@-lsof -ti:8081 | xargs kill -9 2>/dev/null || true
	@sleep 1

demo-nirvana:
	@echo "Starting Nirvana demo server on port 8081..."
	@cd tests/integration/nirvana/cmd && go run . 2>&1 &
	@echo "Nirvana server started (PID: $$!)"

demo-huma:
	@echo "Starting Huma demo server on port 8080..."
	@cd tests/integration/huma/cmd && go run . 2>&1 &
	@echo "Huma server started (PID: $$!)"

test-e2e:
	@echo "Running E2E tests..."
	HUMANIRVANA_HUMA_URL=http://127.0.0.1:8080 HUMANIRVANA_NIRVANA_URL=http://127.0.0.1:8081 $(GOTEST) ./$(TEST)/integration/... -v

# Isolated, test-first migration application; no external server processes required.
DEMO_MODE ?= migrated

demo-tdd:
	$(GOTEST) -race -count=1 ./internal/pilot ./tests/compatibility/...

demo-app:
	$(GOCMD) run ./tests/compatibility/cmd -mode $(DEMO_MODE)

# Clean build artifacts
clean:
	rm -f bin/*
	rm -f coverage.out coverage.html
	rm -f *.test

# Show help
help:
	@echo "huma-nirvana-adapter Build System"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build the adapter"
	@echo "  build-all      Build for all platforms"
	@echo "  test           Run all tests"
	@echo "  coverage       Run tests with coverage report"
	@echo "  cover01        Enforce COVER-01 (library statement coverage >= 80%)"
	@echo "  lint           Run golangci-lint"
	@echo "  demo-e2e       Run demo servers and E2E tests"
	@echo "  demo-tdd       Run isolated migration behavior tests with race detection"
	@echo "  demo-app       Start migration application (DEMO_MODE=migrated|original|rollback)"
	@echo "  clean          Clean build artifacts"
	@echo "  help           Show this help"
