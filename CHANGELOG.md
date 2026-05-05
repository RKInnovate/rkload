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
- JSON Schema for the upcoming v0.3.0 config format at `schemas/v1/config.schema.json`
  with explicit `version` field; example at `docs/examples/basic.config.json`
- URL-based schema versioning policy (`schemas/vN/`, immutable per version) and
  `schemas/README.md` documenting the contract

### Changed
- v0.3.0 configuration format switched from YAML to JSON for first-class
  editor autocomplete via JSON Schema

### Removed
- Stale YAML config sketch (`docs/examples/basic.yaml.example`); will be
  re-introduced as JSON when scenarios land in v0.4.0

## [0.1.0] - TBD

### Added
- Core HTTP load testing engine with goroutine-based concurrency
- CLI flags: `-url`, `-c`, `-n`, `-method`, `-version`
- Latency metrics: average, total time, throughput
- HTTP status code histogram with bar chart
- Worker pool architecture with bounded concurrency

[Unreleased]: https://github.com/RKInnovate/rkload/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/RKInnovate/rkload/releases/tag/v0.1.0
