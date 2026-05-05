# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Common commands

```bash
make build              # build to ./bin/rkload
make test               # go test -v -race -coverprofile=coverage.out ./...
make vet                # go vet ./...
make lint               # vet + staticcheck (auto-installs staticcheck)
make fmt                # gofmt -s -w .
make run ARGS='-url https://example.com -c 10 -n 100'
make release-snapshot   # local goreleaser dry-run (requires goreleaser)
```

Run a single test:

```bash
go test -run TestVersionDefaults ./cmd/rkload
```

CI (`.github/workflows/ci.yml`) runs the test/vet/build matrix on Go 1.22 and 1.23 across Linux/macOS/Windows, plus a lint job that runs `staticcheck` and fails on any unformatted file (`gofmt -l .`). Match this locally with `make lint && make test` before pushing.

## Architecture

### Current state vs. intended layout (important)

The repo's directory layout advertises a clean separation:

```
cmd/rkload/         CLI entry point
internal/loader/    HTTP load engine
internal/report/    aggregation + output
internal/config/    YAML config (planned)
```

**In v0.1.0 the `internal/*` packages are intentionally empty `doc.go` stubs.** The entire engine — flag parsing, worker pool, HTTP execution, aggregation, and output rendering — lives in `cmd/rkload/main.go`. The first task of v0.2.0 is to refactor `main.go` into `internal/loader` and `internal/report` per the layout described in `docs/architecture.md`. When adding functionality, check whether it belongs in the eventual home (loader/report/config) and either place it there or in `main.go` consistent with that future split — don't invent new packages.

### Concurrency model

Fixed-size worker pool over a buffered job channel:

1. Main fills `jobs` (`chan struct{}`, capacity = `-n`) with one token per request, then closes it.
2. `-c` workers `for range jobs`, each owning its own `*http.Client` (30s timeout).
3. Each worker pushes a `result` (duration, status, err) onto `results`.
4. A separate goroutine does `wg.Wait(); close(results)` so the main loop terminates cleanly.
5. Main aggregates from `results` after the channel closes.

The `chan struct{}` (zero-byte token) is deliberate — workers don't need request data yet. This will likely become `chan Request` once YAML config lands in v0.3.0.

### Exit code

Non-zero exit when any request fails — this is load-bearing for CI usage. Preserve it when refactoring.

### Build-time version vars

`version`, `commit`, `date` in `cmd/rkload/main.go` are populated by GoReleaser via `-ldflags` (`.goreleaser.yaml`). They default to `"dev"`/`"none"`/`"unknown"` so a local `-version` invocation works; `TestVersionDefaults` enforces those defaults stay non-empty.

## Conventions

- **Commits:** Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`, `perf:`), optionally scoped (`feat(report): ...`). See `CONTRIBUTING.md`.
- **Stdlib first.** Add a dependency only when stdlib is genuinely insufficient — `go.mod` currently has zero non-stdlib deps.
- **Errors are values.** Failed HTTP requests are recorded as `result{err: ...}` and counted, not panicked on. The run continues.
