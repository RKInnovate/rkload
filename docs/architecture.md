# Architecture

This document describes how rkload is structured internally. It is meant for contributors and curious users.

## Package layout

```
rkload/
├── cmd/rkload/         # CLI entry point — flag parsing and orchestration only
├── internal/
│   ├── loader/         # Core load generation engine
│   ├── report/         # Result aggregation, percentiles, output formatting
│   └── config/         # JSON config parsing and validation (v0.3.0+)
├── schemas/            # JSON Schemas published for editor tooling
└── docs/               # User and contributor documentation
```

The `internal/` directory restricts package visibility — only code within this module can import from it. This keeps the public API surface intentional. If something later needs to be a reusable library, it will move to `pkg/`.

## Data flow

```
┌──────────────┐
│ CLI / Config │   flags + (later) YAML
└──────┬───────┘
       │
       ▼
┌──────────────┐    jobs    ┌──────────────┐  results  ┌──────────────┐
│   Loader     │──────────▶│   Workers    │─────────▶│  Aggregator  │
│  (planner)   │  channel   │ (goroutines) │  channel │  (collector) │
└──────────────┘            └──────────────┘           └──────┬───────┘
                                                              │
                                                              ▼
                                                       ┌──────────────┐
                                                       │   Reporter   │
                                                       │  (formatter) │
                                                       └──────────────┘
```

## Concurrency model

rkload uses a fixed-size worker pool with a buffered job channel.

1. The main goroutine fills a buffered `jobs` channel with N tokens (one per request) and closes it.
2. C worker goroutines each loop `for range jobs`, draining the queue.
3. Each worker performs an HTTP request and pushes a `result` onto the `results` channel.
4. A separate goroutine waits on `wg.Wait()` and closes the `results` channel once all workers exit.
5. The main goroutine reads from `results` until it is closed, then computes metrics.

This design provides:

- **Deterministic concurrency** — exactly C in-flight requests at any moment
- **Backpressure** — workers naturally throttle when the server is slow
- **Clean shutdown** — closing the job channel is the only signal needed
- **No leaked goroutines** — the close-after-wait pattern guarantees termination

## Why the empty struct?

The job channel uses `chan struct{}` rather than `chan int`. The empty struct (`struct{}{}`) is a zero-byte type — it carries no data, only signals existence. Workers don't care about the contents of a job at this stage; they just need a token telling them "do one request." When JSON configuration arrives in v0.3.0, this will likely change to `chan Request`.

## Configuration format

Starting in v0.3.0, rkload reads a JSON config file describing endpoints grouped by HTTP method. The format is defined by [`schemas/v1/config.schema.json`](../schemas/v1/config.schema.json) (Draft 2020-12) and carries a top-level integer `version` field so the schema can evolve. Editors that consume the `$schema` URL get autocomplete and validation for free; the runtime rejects unknown versions and reports schema violations with the offending JSON pointer. See [`docs/examples/basic.config.json`](./examples/basic.config.json) for a worked example.

### Schema versioning

Each schema version lives at its own immutable path (`schemas/v1/`, `schemas/v2/`, …) and is **never modified once published**. User configs MUST pin a versioned `$schema` URL; the top-level `version` integer in the config MUST match the version segment of that URL, and the runtime cross-checks both. There is no "latest" alias — pinning a version is the contract that lets old configs keep validating correctly long after newer schema versions ship. Full policy in [`schemas/README.md`](../schemas/README.md).

## Design principles

1. **Stdlib first.** External dependencies are added only when stdlib is genuinely insufficient.
2. **Small surface area.** Each package has one job. If a function does two things, it splits.
3. **Errors are values.** Failed requests are recorded as data, not panics. The tool keeps running.
4. **CI-friendly.** Non-zero exit on errors. Machine-readable output formats coming in v0.5.

## Where things live

| Concern                | Home                                  |
|------------------------|---------------------------------------|
| HTTP execution         | `internal/loader/loader.go`           |
| Result aggregation     | `internal/report/report.go`           |
| Percentile computation | `internal/report/percentile.go`       |
| Error classification   | `internal/report/errorclass.go`       |
| Latency distribution   | `internal/report/distribution.go`     |
| JSON config (v0.3.0+)  | `internal/config/` (schema in `schemas/v1/`) |
| Scenarios / chains     | `internal/scenario/` (v0.4.0)         |
| Auth helpers           | `internal/auth/` (v0.4.0)             |
| Output formats         | `internal/report/format_*.go` (v0.5.0)|

`cmd/rkload/main.go` is intentionally minimal: flag parsing, building a `loader.Options`, calling `loader.Run`, handing the results to `report.Summarize` + `report.Print`, and applying the exit-code policy. Anything else belongs in `internal/`.
