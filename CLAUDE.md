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

### Layout

```
cmd/rkload/                     flag parsing + runSingle (-url) + runFromConfig (-config)
                                + importMain dispatcher (rkload import …)
internal/loader/                Options (incl. Headers + Body), Result, Run
internal/report/                Summary, Summarize, Print — aggregation + rendering
internal/report/percentile.go   min/max/p50/p95/p99/stddev (nearest-rank)
internal/report/errorclass.go   ErrorClass + Classify (timeout/conn refused/DNS/TLS/other)
internal/report/distribution.go Bucket + linear histogram between min and max
internal/config/                Config, Endpoint, Load, Validate, Groups — schema v1
internal/importer/              OpenAPI 3.x + Swagger 2.0 → *config.Config
                                (Postman v2.1 lands in v0.3.2)
schemas/vN/                     published JSON Schemas, immutable per version
docs/examples/                  worked example configs
```

**Flag ordering for subcommands:** Go's stdlib `flag` package stops at the first positional argument, so `rkload import openapi <spec> --tag x` won't parse `--tag` — flags must precede the spec path (`--tag x <spec>`). Mentioned here because it surprises users coming from `getopt`-style CLIs.

**External deps:** `gopkg.in/yaml.v3` is the only one (added with the importer for YAML spec support). Anything else should still go through "stdlib first" justification.

**Schema versioning is URL-based and immutable.** A published schema file (e.g. `schemas/v1/config.schema.json`) is never modified once shipped — breaking changes go to a new `schemas/v2/` directory. User configs MUST pin a versioned `$schema` URL; the top-level `version` integer must match the URL's `vN` segment. There is intentionally no "latest" alias. See `schemas/README.md` for the full policy and update it when adding `v2`.

`cmd/rkload/main.go` is deliberately small: parse flags → build `loader.Options` → `loader.Run` → `report.Summarize` → `report.Print` → exit-code policy. New behavior belongs in `internal/`, not in `main.go`.

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
