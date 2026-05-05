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

### Removed
- Stale YAML config sketch (`docs/examples/basic.yaml.example`); will be
  re-introduced as JSON when scenarios land in v0.4.0

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

[Unreleased]: https://github.com/RKInnovate/rkload/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/RKInnovate/rkload/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/RKInnovate/rkload/releases/tag/v0.1.0
