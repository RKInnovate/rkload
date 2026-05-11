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

## v0.3.1 — OpenAPI / Swagger import ✅

Generate rkload configs from existing API specifications so teams with OpenAPI specs don't hand-write `rkload.json`.

- [x] `rkload import openapi <spec>` subcommand reading JSON or YAML
- [x] OpenAPI 3.x support
- [x] Swagger 2.0 support (legacy but common)
- [x] Map `paths.{path}.{method}` → endpoint, with URL = `servers[].url` (or schemes/host/basePath for Swagger 2) + path
- [x] `operationId` → endpoint `name` (falls back to `method-path-with-dashes`)
- [x] `requestBody.content."application/json".example` (OpenAPI 3) and `parameters[in=body].x-example`/`example` (Swagger 2) → endpoint `body`
- [x] Filter flags: `--tag`, `--path-prefix`
- [x] Defaults for `c` / `requests` / `timeout` from CLI flags so the generated file is immediately runnable
- [x] Auth headers emitted as `REPLACE_ME` placeholders (specs don't carry real tokens)
- [x] Deterministic output (paths sorted lexically, methods in fixed order) so re-runs produce byte-identical files

---

## v0.3.2 — Postman import ✅

- [x] `rkload import postman <collection>` subcommand
- [x] Postman Collection v2.1 support
- [x] Map `item[].request` → endpoints (method, URL, headers, body)
- [x] Folder flattening — nested `item[].item[]` produces a flat config
- [x] `{{var}}` substitution from collection-level `variable[]`
- [x] `--var key=value` (repeatable) for user-supplied overrides
- [x] Disabled headers dropped (matches Postman's own send behaviour)
- [x] Same defaults / `--path-prefix` flags as the OpenAPI importer
- [x] Raw body mode supported; formdata / urlencoded / file out of scope

---

## v0.3.3 — Validate subcommand ✅

A standalone validation step that doubles as a record-keeping cache, so large configs don't re-validate on every run.

- [x] `rkload validate <config>` subcommand prints a one-screen summary (path, canonical hash, file size, schema URL/version, per-method endpoint counts) and returns non-zero on any validation failure
- [x] `internal/cache` package storing `~/.rkload/cache/<sha256>.json` entries keyed by canonical JSON hash (sorted-key, whitespace-invariant)
- [x] `--no-cache` flag on `validate` to skip both read and write — useful in CI
- [x] `RKLOAD_CACHE_DIR` environment variable to redirect the cache (per-project isolation, sandboxed tests)
- [x] `-config` run flow consults the cache automatically: hash hit + rkload-version match skips re-validation; miss / mismatch re-validates and refreshes the entry
- [x] Cache write failures reported inline, never fatal — validation succeeded; only the bookkeeping didn't
- [x] `config.Parse` + exported `Config.ApplyDefaults` so the CLI can split parse-from-validate around the cache lookup

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
