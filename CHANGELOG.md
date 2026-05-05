# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial repository scaffolding
- MIT license, contributing guide, code of conduct
- GitHub Actions CI pipeline (vet, staticcheck, test, build)
- GoReleaser configuration for cross-platform release builds
- Makefile with common dev targets
- `.editorconfig`, `.vscode/`, and `.zed/` for shared formatting across editors
- JSON Schema for the upcoming v0.3.0 config format at `schemas/v1/config.schema.json`
  with explicit `version` field; example at `docs/examples/basic.config.json`
- URL-based schema versioning policy (`schemas/vN/`, immutable per version) and
  `schemas/README.md` documenting the contract

### Changed
- v0.3.0 configuration format switched from YAML to JSON for first-class
  editor autocomplete via JSON Schema

### Fixed
- CI Test job on `windows-latest` now pins `shell: bash` so PowerShell stops
  splitting `-coverprofile=coverage.out` and handing `.out` to `go test` as a
  package path

### Changed (CI)
- Test matrix reduced from Go 1.22+1.23 to Go 1.23 only (3 jobs instead of 6).
  `go.mod` still declares 1.22 as the minimum so 1.22 users can install via
  `go install`, but CI no longer verifies that path

### Roadmap
- Added v0.3.1 (OpenAPI / Swagger import) and v0.3.2 (Postman import) sections
  for generating rkload configs from existing API specifications

### Removed
- Stale YAML config sketch (`docs/examples/basic.yaml.example`); will be
  re-introduced as JSON when scenarios land in v0.4.0

## [0.3.2] - TBD

### Added
- `internal/importer.Postman(io.Reader, PostmanOptions) (*config.Config, error)`
  for Postman Collection v2.1. Folder nesting flattened, collection
  `variable[]` substituted into `{{var}}` references, custom
  `UnmarshalJSON` on the URL field handles both string and object
  shapes
- `rkload import postman <collection>` subcommand mirroring the
  openapi handler, plus a repeatable `--var key=value` flag for
  user-supplied variable overrides (overrides win over collection
  vars; unknown vars pass through verbatim so they're greppable)
- Tiny `repeatableFlag` helper (custom `flag.Value`) so `--var` can
  appear multiple times — stdlib has no `StringSlice` equivalent

### Limitations
- Only `body.mode: "raw"` is extracted; formdata / urlencoded / file
  modes silently produce empty bodies (would require richer
  `Endpoint` body support)
- Collection schema URL must contain `v2.1`; v2.0 collections are
  rejected with a clear error

## [0.3.1] - TBD

### Added
- `internal/importer` package: `OpenAPI(io.Reader, OpenAPIOptions) (*config.Config, error)`
  auto-detects OpenAPI 3.x vs Swagger 2.0 by inspecting the top-level
  `openapi`/`swagger` key and parses both into a v1 rkload Config that
  passes `config.Validate` immediately
- `rkload import openapi <spec>` subcommand with `-o` (output file,
  default stdout), `-c`/`-n`/`-timeout` (per-endpoint defaults),
  `--tag` and `--path-prefix` filters
- YAML support via `gopkg.in/yaml.v3` (the project's first external
  dependency). Format detected from the first non-whitespace byte;
  YAML inputs are converted to JSON in-memory so the spec parsers
  only ever see canonical JSON
- Generated configs pin the canonical `$schema` URL and emit
  `Authorization: REPLACE_ME` placeholders for any operation with a
  security requirement so users can grep across hundreds of endpoints
- Deterministic output: paths sorted lexically, methods in fixed
  order, byte-identical re-runs

### Changed
- `cmd/rkload/main.go` now dispatches `rkload import …` to a
  separate handler before top-level flag parsing, so positional
  args after a subcommand don't trip the root flag set

## [0.3.0] - TBD

### Added
- `internal/config` package: `Load`, `Validate`, `Endpoint`, `Config`,
  `Groups()`, defaults. Validates against schema v1, cross-checks the
  `$schema` URL's `vN` segment against the `version` integer, and uses
  `json.Decoder.DisallowUnknownFields` so typos and unsupported method
  keys (e.g. `TRACE`) fail fast instead of being silently dropped
- `loader.Options.Headers` and `loader.Options.Body`: per-request
  headers and a body string the loader applies on every iteration
  (a fresh `strings.NewReader` per request so the same body string
  can drive hundreds of concurrent workers safely)
- `-config <path>` CLI flag for multi-endpoint runs, mutually exclusive
  with `-url`. Endpoints run sequentially per-method-group with
  individual reports, plus an `=== Overall ===` aggregate. Exit code
  remains 1 if any endpoint had any failed request — same CI semantic
  as the existing `-url` path
- testdata fixtures for valid configs, version-mismatch, and
  unknown-field rejection

### Changed
- `cmd/rkload/main.go` factored into `runSingle` (existing `-url`
  flow, behaviour-preserved) and `runFromConfig` (new). The "neither
  given" error message now lists both forms as examples

## [0.2.0] - TBD

### Added
- Latency percentiles (p50, p95, p99), min, max, and population standard
  deviation alongside the existing average
- Failed request grouping by error class — timeout, connection refused, DNS,
  TLS, other — using `errors.Is` / `errors.As` against the real http.Client
  error chain
- Linear-bucketed ASCII latency distribution histogram (10 buckets between
  min and max), with the final bucket stretched to absorb the maximum
- New exported types in `internal/report`: `LatencyStats`, `ErrorClass` (+
  `Classify`), `Bucket`

### Changed
- Refactored the v0.1.0 single-file engine into `internal/loader`
  (`Options`, `Result`, `Run`) and `internal/report` (`Summary`,
  `Summarize`, `Print`). `cmd/rkload/main.go` is now flag parsing +
  orchestration only. No behavior change for end users.
- `Print` now writes to an `io.Writer` so future formats (JSON, Markdown)
  can plug in without touching `os.Stdout`
- Latency output regrouped under a single "Latency:" block with `avg`,
  `min`, `max`, `p50`, `p95`, `p99`, `stddev` rows

## [0.1.0] - TBD

### Added
- Core HTTP load testing engine with goroutine-based concurrency
- CLI flags: `-url`, `-c`, `-n`, `-method`, `-version`
- Latency metrics: average, total time, throughput
- HTTP status code histogram with bar chart
- Worker pool architecture with bounded concurrency

[Unreleased]: https://github.com/RKInnovate/rkload/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/RKInnovate/rkload/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/RKInnovate/rkload/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/RKInnovate/rkload/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/RKInnovate/rkload/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/RKInnovate/rkload/releases/tag/v0.1.0
