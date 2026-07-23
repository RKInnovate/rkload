# Roadmap

This document tracks the planned evolution of rkload. Versions follow [Semantic Versioning](https://semver.org/).

Items here are aspirational, not commitments. Priorities shift based on real usage and community feedback.

---

## Shipped so far (0.x)

Everything below is implemented and available on the 0.x release line (tagged through v0.3.2). The project deliberately stays in 0.x while the CLI and config surface settle — additive features ship as 0.x minors, and a v1.0.0 major is reserved for the first stable release.

### Live TUI dashboard
- [x] Auto-on when stdout is a TTY, plain-text path preserved everywhere else
- [x] Per-endpoint progress bar with counter, throughput, live p95
- [x] Aggregate status code distribution coloured by class
- [x] Rolling p50/p95/p99 latency ticker
- [x] Throughput sparkline over the recent past
- [x] Keybindings: q to quit, ↑↓ to select, ↵/→ to drill in, esc/← to back out
- [x] Plain-text aggregate report still prints after TUI exits so `| tee log.txt` keeps working

### Multi-config loading
- [x] `-config <dir>` scans for `*.rkload.json` (compound suffix avoids loading unrelated JSON), lexical order, non-recursive
- [x] All configs run as one combined session under a single TUI and single aggregate report

### Engine
- [x] Goroutine-based concurrent HTTP load generation with a bounded worker pool
- [x] CLI flags: `-url`, `-c`, `-n`, `-method`, `-version`, `-config`, `-help`
- [x] Aggregate metrics: total time, throughput, average latency, status-code histogram

### Reporting
- [x] p50 / p95 / p99 latency percentiles, min, max, population standard deviation
- [x] Failed request grouping by error class (timeout / connection refused / DNS / TLS / other)
- [x] Linear-bucketed ASCII latency distribution histogram

### Multi-endpoint configuration (schema v1)
- [x] JSON config file ([`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json)) with strict `additionalProperties: false`
- [x] URL-based schema versioning (`schemas/vN/...` immutable per version, [`schemas/README.md`](./schemas/README.md))
- [x] Per-endpoint `name`, `headers`, `body`, `c`, `requests`, `timeout` overrides
- [x] `-config <path>` flag, sequential per-method-group runs with per-endpoint reports and an `=== Overall ===` aggregate

### Spec import
- [x] `rkload import openapi <spec>` for OpenAPI 3.x and Swagger 2.0 (JSON or YAML, auto-detected)
- [x] `--server-url` / `--server-index` to override the spec's base URL
- [x] `--tag` and `--path-prefix` filters
- [x] `rkload import postman <collection>` for Postman Collection v2.1 with folder flattening, `{{var}}` substitution, and repeatable `--var key=value`
- [x] Deterministic output (paths sorted lexically, methods in fixed order)

### Scaffolding & validation
- [x] `rkload init [path] [--force]` writes a starter config so users without an OpenAPI / Postman input can begin from a working file
- [x] `rkload validate <config>` with a canonical-JSON content-hash cache at `~/.rkload/cache/`
- [x] Cache-aware `-config` run flow that skips redundant re-validation; cache write failures are reported inline without failing the run

### Self-update
- [x] `rkload update` subcommand: GitHub Releases API discovery (with redirect fallback), SHA-256 verification against `checksums.txt`, atomic in-place replacement
- [x] `--check` (report only), `--version vX.Y.Z` (pin / downgrade), `--force` (reinstall when current)
- [x] Daily background "update available" notice on startup; opt out via `RKLOAD_NO_UPDATE_CHECK=1` or non-tty stdout

### Tooling
- [x] One-line installers (`scripts/install.sh`, `scripts/install.ps1`) that fetch the matching release archive and put `rkload` on `PATH`
- [x] GoReleaser config for cross-platform binary releases on tag push
- [x] CI matrix across Linux / macOS / Windows with vet, staticcheck, race-mode tests

---

## v0.4.0 — Scenarios

The feature that justifies the name "scenario-driven."

- [ ] Multi-step request chains (login → call → logout)
- [ ] Variable extraction from response (JSONPath / regex)
- [ ] Variable injection into subsequent requests
- [ ] Auth helpers: Bearer token, API key, Basic auth, OAuth2 client credentials
- [ ] Simple assertion DSL (status code, response body contains, JSON field equals)

---

## v0.5.0 — Reporting & integration

Make rkload useful in CI and team workflows.

- [ ] Output formats: JSON, Markdown, HTML
- [ ] Comparison mode (diff two runs)
- [ ] CI exit codes based on configurable thresholds
- [ ] Optional live TUI dashboard
- [ ] Prometheus-compatible metrics export (stretch)

---

## Future hardening

Targets that aren't tied to a specific release yet:

- [ ] Documentation site (separate from README)
- [ ] Performance benchmarks vs comparable tools
- [ ] Schema-based example body synthesis in `rkload import openapi` (so specs with `$ref`-only `requestBody` produce non-empty bodies)
- [ ] `--auth-header NAME:VALUE` on importers so specs without declared `security` still get auth injected

---

## v1.0.0 — Reserved

The first stable release. Cut once the CLI surface and config schema have settled; until then, additive features ship as 0.x minors and breaking changes are allowed between them. Config schema versions evolve independently under `schemas/vN/`.

---

## Beyond v1.0 — Maybe

Ideas being considered, not committed:

- Distributed load generation (multi-machine coordination)
- gRPC support
- WebSocket load testing
- Built-in chaos hooks (delay, drop, mutate)
- Plugin system for custom auth / payload generators
