# Roadmap

This document tracks the planned evolution of rkload. Versions follow [Semantic Versioning](https://semver.org/).

Items here are aspirational, not commitments. Priorities shift based on real usage and community feedback.

---

## v0.1.0 — Core engine ✅

The minimum viable load tester.

- [x] Goroutine-based concurrent HTTP load generation
- [x] Bounded worker pool via buffered channels
- [x] CLI flags for URL, concurrency, request count, method
- [x] Aggregate metrics: total time, throughput, average latency
- [x] HTTP status code histogram

---

## v0.2.0 — Better metrics

Make the report tell a real story, not just an average.

- [ ] p50 / p95 / p99 latency percentiles
- [ ] Min / max latency
- [ ] Standard deviation
- [ ] Failed request grouping by error type
- [ ] Sorted latency distribution output

---

## v0.3.0 — Configuration

Move beyond single-URL tests.

- [ ] JSON config file support (schema v1, see [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json))
- [ ] Multiple endpoints per run, grouped by HTTP method
- [ ] Per-endpoint headers (Authorization, User-Agent, etc.)
- [ ] Per-endpoint request body
- [ ] Per-endpoint concurrency, request count, and timeout overrides
- [ ] URL-based schema versioning (`schemas/vN/...` immutable per version, see [`schemas/README.md`](./schemas/README.md))

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

## v1.0.0 — Stable

The contract for a 1.0:

- [ ] Comprehensive test coverage (≥80%)
- [ ] Stable public API surface
- [ ] Documentation site
- [ ] Performance benchmarks vs comparable tools
- [ ] Production usage at multiple organizations

---

## Beyond 1.0 — Maybe

Ideas being considered, not committed:

- Distributed load generation (multi-machine coordination)
- gRPC support
- WebSocket load testing
- Built-in chaos hooks (delay, drop, mutate)
- Plugin system for custom auth / payload generators
