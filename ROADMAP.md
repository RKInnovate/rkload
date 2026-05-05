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

## v0.2.0 — Better metrics ✅

Make the report tell a real story, not just an average.

- [x] p50 / p95 / p99 latency percentiles
- [x] Min / max latency
- [x] Standard deviation
- [x] Failed request grouping by error class (timeout / connection refused / DNS / TLS / other)
- [x] Linear-bucketed latency distribution histogram
- [x] Engine extracted to `internal/loader`, reporting to `internal/report`

---

## v0.3.0 — Configuration ✅

Move beyond single-URL tests.

- [x] JSON config file support (schema v1, see [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json))
- [x] Multiple endpoints per run, grouped by HTTP method
- [x] Per-endpoint headers (Authorization, User-Agent, etc.)
- [x] Per-endpoint request body
- [x] Per-endpoint concurrency, request count, and timeout overrides
- [x] URL-based schema versioning (`schemas/vN/...` immutable per version, see [`schemas/README.md`](./schemas/README.md))
- [x] `version` integer cross-checked against the `$schema` URL's `vN` segment
- [x] `-config <path>` CLI flag, mutually exclusive with `-url`

---

## v0.3.1 — OpenAPI / Swagger import

Generate rkload configs from existing API specifications so teams with OpenAPI specs don't hand-write `rkload.json`.

- [ ] `rkload import openapi <spec>` subcommand reading JSON or YAML
- [ ] OpenAPI 3.0 / 3.1 support
- [ ] Swagger 2.0 support (legacy but common)
- [ ] Map `paths.{path}.{method}` → endpoint, with URL = `servers[].url` + path
- [ ] `operationId` (fallback `summary`) → endpoint `name`
- [ ] `requestBody.content."application/json".example` → endpoint `body`
- [ ] Filter flags: `--tag`, `--path-prefix`, `--method` so a 200-endpoint spec doesn't load-test all of them
- [ ] Defaults for `c` / `requests` / `timeout` from CLI flags so the generated file is immediately runnable
- [ ] Auth headers emitted as `REPLACE_ME` placeholders (specs don't carry real tokens)

---

## v0.3.2 — Postman import

- [ ] `rkload import postman <collection>` subcommand
- [ ] Postman Collection v2.1 support
- [ ] Map `item[].request` → endpoints (method, URL, headers, body)
- [ ] Translate Postman `{{var}}` references to the env interpolation that ships in v0.3.0
- [ ] Same filter / defaults flags as the OpenAPI importer

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
