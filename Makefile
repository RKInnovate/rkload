.PHONY: help build test vet fmt lint clean install run release-snapshot release-local

BINARY      := rkload
MAIN        := ./cmd/rkload
BIN_DIR     := bin
COVERAGE    := coverage.out

# Default target prints help
help:
	@echo "rkload — common development targets"
	@echo ""
	@echo "  make build              Build binary to ./bin/$(BINARY)"
	@echo "  make install            Install to GOPATH/bin"
	@echo "  make test               Run tests with race detector and coverage"
	@echo "  make vet                Run go vet"
	@echo "  make lint               Run staticcheck (installs if missing)"
	@echo "  make fmt                Format all Go files with gofmt"
	@echo "  make clean              Remove build artifacts"
	@echo "  make run ARGS='...'     Build and run with ARGS"
	@echo "  make release-snapshot   Test goreleaser config locally"
	@echo "  make release-local      Build + publish a real GitHub Release from this machine"
	@echo "                          (use when the GitHub Actions release workflow is unavailable)"

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY) $(MAIN)

install:
	go install $(MAIN)

test:
	go test -v -race -coverprofile=$(COVERAGE) ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint: vet
	@which staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...

clean:
	rm -rf $(BIN_DIR)/ dist/ $(COVERAGE) coverage.html

run: build
	./$(BIN_DIR)/$(BINARY) $(ARGS)

# Test goreleaser config without publishing.
# Uses `go run` so no global goreleaser install is required.
release-snapshot:
	go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean

# Publish a real GitHub Release from this machine. Use when the
# GitHub Actions release workflow isn't firing (e.g. while we
# debug it) and you still want artefacts attached to a tag.
#
# Requirements:
#   - working tree clean and on the tag you want to release
#     (e.g. `git checkout v1.0.0`)
#   - GITHUB_TOKEN env var set to a PAT with `repo` scope
#     (goreleaser uses it to create the Release and upload assets)
#
# Run from the repo root:
#   GITHUB_TOKEN=ghp_xxx make release-local
#
# This is identical to what the GitHub Actions workflow does; the
# only difference is who holds the token.
release-local:
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "release-local: GITHUB_TOKEN is not set"; \
		echo "  Create a PAT with 'repo' scope at https://github.com/settings/tokens"; \
		echo "  Then: GITHUB_TOKEN=ghp_xxx make release-local"; \
		exit 1; \
	fi
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "release-local: working tree is dirty; commit or stash first"; \
		exit 1; \
	fi
	go run github.com/goreleaser/goreleaser/v2@latest release --clean
