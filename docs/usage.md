# Usage Guide

## Basic usage

```bash
rkload -url https://api.example.com/health -c 50 -n 1000
```

This sends 1000 GET requests using 50 concurrent workers.

## Flags

| Flag | Default | Description |
|---|---|---|
| `-url` | _(required)_ | Target URL |
| `-c` | `10` | Number of concurrent workers |
| `-n` | `100` | Total number of requests |
| `-method` | `GET` | HTTP method |
| `-version` | `false` | Print version and exit |

## Reading the output

```
Load testing: https://api.example.com/health
Workers: 50 | Requests: 1000 | Method: GET

--- Results ---
Total requests:  1000
Successful:      1000
Errors:          0
Total time:      5.563s
Throughput:      179.77 req/sec
Avg latency:     545ms

Status codes:
  HTTP 200: 1000 ████████████████████
```

- **Total time** — wall clock for the entire test
- **Throughput** — requests per second sustained across the full test
- **Avg latency** — mean response time across successful requests only
- **Status codes** — distribution of HTTP response codes (bar is relative to total successful)

> Average latency hides cold starts and tail latency. Percentile reporting (p50/p95/p99) lands in v0.2.0 and is what you actually want to look at before going to production.

## Choosing concurrency

A reasonable warmup approach:

1. Start at `-c 10`
2. Double until you observe one of:
   - **Latency starts climbing sharply** — your server is saturating
   - **Errors appear** — connection limits, timeouts, or rate limiting kicked in
   - **Throughput plateaus** — diminishing returns, more workers won't help

The concurrency at which latency begins to climb is your effective production ceiling.

## Choosing request count

Rule of thumb: `-n` should be at least 10× your concurrency. With `-c 50`, use `-n 500` minimum. This gives each worker time to settle and produces stable averages.

For environments with cold starts (Cloud Run, Lambda, Cloud Functions), use `-n` of 1000+ so the cold-start window is a smaller fraction of the dataset.

## Practical recipes

### Smoke test before deploy

```bash
rkload -url https://staging.api.example.com/health -c 10 -n 100
```

Quick check that the service is alive and responsive.

### Saturation test

```bash
rkload -url https://staging.api.example.com/ -c 200 -n 10000
```

Find the point where the service can no longer keep up.

### Throughput baseline

```bash
rkload -url https://staging.api.example.com/ -c 100 -n 5000
```

Establishes a steady-state throughput number you can track over time.

## Tips

- Run from a machine geographically close to the target. Network RTT can dominate measurements otherwise.
- Run multiple times. The first run after deployment usually includes cold-start cost.
- For cloud autoscaled services, run a "warmup" test first, then your real measurement.
- Consider whether you actually want to test the root or health endpoint, or your real API workflow. The latter requires scenarios (v0.4.0).

## Coming soon

The next versions will add:

- **v0.2.0** — `-output json` for machine-readable results, p50/p95/p99 percentiles
- **v0.3.0** — `-config rkload.json` for multi-endpoint suites (schema v1, see [`schemas/v1/config.schema.json`](../schemas/v1/config.schema.json) and the [example](./examples/basic.config.json); versioning policy in [`schemas/README.md`](../schemas/README.md))
- **v0.4.0** — Multi-step scenarios with auth chains

See [ROADMAP.md](../ROADMAP.md) for the full plan.
