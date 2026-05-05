# Architecture

This document describes how rkload is structured internally. It is meant for contributors and curious users.

## Package layout

```
rkload/
├── cmd/rkload/         # CLI entry point — flag parsing and orchestration only
├── internal/
│   ├── loader/         # Core load generation engine
│   ├── report/         # Result aggregation, percentiles, output formatting
│   └── config/         # YAML config parsing and validation (v0.3.0+)
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

The job channel uses `chan struct{}` rather than `chan int`. The empty struct (`struct{}{}`) is a zero-byte type — it carries no data, only signals existence. Workers don't care about the contents of a job at this stage; they just need a token telling them "do one request." When configuration arrives in v0.3.0, this will likely change to `chan Request`.

## Design principles

1. **Stdlib first.** External dependencies are added only when stdlib is genuinely insufficient.
2. **Small surface area.** Each package has one job. If a function does two things, it splits.
3. **Errors are values.** Failed requests are recorded as data, not panics. The tool keeps running.
4. **CI-friendly.** Non-zero exit on errors. Machine-readable output formats coming in v0.5.

## Where things will live as the project grows

| Concern | Today | Future home |
|---|---|---|
| HTTP execution | `cmd/rkload/main.go` | `internal/loader/loader.go` |
| Result aggregation | `cmd/rkload/main.go` | `internal/report/report.go` |
| Percentile computation | _(not yet)_ | `internal/report/percentile.go` |
| YAML config | _(not yet)_ | `internal/config/config.go` |
| Scenarios / chains | _(not yet)_ | `internal/scenario/scenario.go` |
| Auth helpers | _(not yet)_ | `internal/auth/auth.go` |

The current single-file implementation is intentional for v0.1.0. Refactoring into the layout above is the first task of v0.2.0.
