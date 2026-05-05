.PHONY: help build test vet fmt lint clean install run release-snapshot

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

# Test goreleaser config without publishing
release-snapshot:
	@which goreleaser >/dev/null 2>&1 || (echo "goreleaser not installed: https://goreleaser.com/install/" && exit 1)
	goreleaser release --snapshot --clean
