# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_Nothing yet — open the next section once changes land._

## [1.0.0] - 2026-05-11

First published release. All prior version numbers existed as
development markers in the local git history but were never built
into GitHub Releases, so v1.0.0 is the first version users can
install and self-update against.

### Added

#### Engine
- Concurrent HTTP load testing with a goroutine-based worker pool
  bounded by `-c`. CLI flags `-url`, `-c`, `-n`, `-method`,
  `-version`, `-config`, `-help`
- Throughput, total elapsed, status-code histogram

#### Reporting
- Latency percentiles (p50, p95, p99), min, max, population
  standard deviation alongside the average
- Failed request grouping by error class — timeout, connection
  refused, DNS, TLS, other — using `errors.Is` / `errors.As`
  against the real `http.Client` error chain
- Linear-bucketed ASCII latency distribution histogram (10
  buckets between min and max, final bucket stretched to absorb
  the maximum)

#### Multi-endpoint configuration (schema v1)
- `internal/config` package with `Parse`, `Validate`, `Load`, and
  exported `ApplyDefaults`. Strict JSON decoding rejects unknown
  fields so typos surface immediately
- JSON Schema at `schemas/v1/config.schema.json`, immutable
  per-version policy documented in `schemas/README.md`. The
  config's `version` integer is cross-checked against the
  `$schema` URL's `vN` segment
- `-config <path>` CLI flag for multi-endpoint runs (mutually
  exclusive with `-url`). Endpoints run sequentially per method
  group with per-endpoint reports plus an `=== Overall ===`
  aggregate. Exit code 1 if any endpoint had any failed request
- Per-endpoint `headers` and `body` flowed through the loader on
  every iteration via a fresh `strings.NewReader` per request so
  the same body string can drive hundreds of concurrent workers
  safely

#### Spec import
- `rkload import openapi <spec>` for OpenAPI 3.x and Swagger 2.0
  (JSON or YAML, auto-detected). Operations become endpoints
  grouped by HTTP method; `operationId` → name; `requestBody`
  example → body; security requirements → `Authorization:
  REPLACE_ME` placeholder. Deterministic output (paths sorted
  lexically, methods in fixed order)
- `--server-url URL` and `--server-index N` overrides for specs
  that list a development server first
- `--tag` and `--path-prefix` filters
- `rkload import postman <collection>` for Postman Collection
  v2.1. Folder nesting flattened, collection-level `variable[]`
  substituted into `{{var}}` references; repeatable
  `--var key=value` for user-supplied overrides
- YAML support via `gopkg.in/yaml.v3` (the project's only
  external dependency, used as a converter — every spec is
  re-encoded as JSON in memory before parsing)

#### Scaffolding
- `rkload init [path] [--force]` writes a starter config (one
  GET, one POST with headers/body/timeout, `$schema` pinned) to
  a file or stdout

#### Validation cache
- `rkload validate <config>` runs full schema validation, prints
  a one-screen summary (hash, file size, schema URL/version,
  per-method endpoint counts), and records the result in
  `~/.rkload/cache/<sha256>.json`. `--no-cache` skips read+write;
  `RKLOAD_CACHE_DIR` redirects the cache
- Cache-aware `-config` flow: hash-matched configs skip
  re-validation; cache misses or rkload-version mismatches
  re-validate and refresh the entry. Cache write failures
  reported inline without flipping the exit code
- Canonical JSON hashing means reformatting or key-order changes
  don't invalidate the cache — only semantic edits do

#### Self-update
- `rkload update` discovers the latest GitHub Release (API
  primary, redirect fallback for rate-limited callers),
  downloads the host-matching GoReleaser archive, verifies its
  SHA-256 against the published `checksums.txt`, and atomically
  replaces the running binary. Unix uses `os.Rename`; Windows
  shuffles via `<path>.old` because the running .exe can't be
  overwritten. Symlinks are resolved before the swap so
  `/usr/local/bin/rkload`-style symlinks update the real file
- Flags: `--check` (dry-run discovery), `--version vX.Y.Z` (pin
  or downgrade), `--force` (reinstall when already at latest)
- Daily background "update available" notice on startup. Silent
  on every failure; skipped for non-tty stdout, `version=="dev"`,
  and `RKLOAD_NO_UPDATE_CHECK=1`. State persists at
  `~/.rkload/update.json`; second invocation within 24h doesn't
  touch the network

#### Tooling
- `scripts/install.sh` (Linux/macOS) and `scripts/install.ps1`
  (Windows) one-line installers that fetch the matching
  GoReleaser archive from GitHub Releases, install `rkload` to a
  PATH-visible location, and verify with `rkload -version`. Pin
  a release via `--version v1.0.0`; redirect via `--dir DIR` or
  `RKLOAD_INSTALL_DIR`
- GitHub Actions CI pipeline (vet, staticcheck, test, build)
  matrix across Linux/macOS/Windows
- GoReleaser configuration for cross-platform binary releases on
  tag push
- Makefile with build/test/lint/release-snapshot targets
- `.editorconfig`, `.vscode/`, and `.zed/` for shared formatting
  across editors
- MIT licence, contributing guide, code of conduct

[Unreleased]: https://github.com/RKInnovate/rkload/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/RKInnovate/rkload/releases/tag/v1.0.0
