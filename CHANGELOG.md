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

## [0.1.0] - TBD

### Added
- Core HTTP load testing engine with goroutine-based concurrency
- CLI flags: `-url`, `-c`, `-n`, `-method`, `-version`
- Latency metrics: average, total time, throughput
- HTTP status code histogram with bar chart
- Worker pool architecture with bounded concurrency

[Unreleased]: https://github.com/RKInnovate/rkload/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/RKInnovate/rkload/releases/tag/v0.1.0
