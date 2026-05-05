# Usage Guide

## Basic usage

```bash
rkload -url https://api.example.com/health -c 50 -n 1000
```

This sends 1000 GET requests using 50 concurrent workers.

## Flags

| Flag       | Default       | Description                                                     |
|------------|---------------|-----------------------------------------------------------------|
| `-url`     | _(required¹)_ | Target URL (single-endpoint mode)                               |
| `-config`  | _(required¹)_ | Path to a JSON config (multi-endpoint mode, see below)          |
| `-c`       | `10`          | Number of concurrent workers (single-endpoint mode)             |
| `-n`       | `100`         | Total number of requests (single-endpoint mode)                 |
| `-method`  | `GET`         | HTTP method (single-endpoint mode)                              |
| `-version` | `false`       | Print version and exit                                          |

¹ Exactly one of `-url` or `-config` is required.

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

Latency:
  avg:    545ms
  min:    368ms
  max:    1.688s
  p50:    488ms
  p95:    1.207s
  p99:    1.588s
  stddev: 169ms

Status codes:
  HTTP 200: 1000 ████████████████████

Latency distribution:
     368ms - 500ms   :   823 ██████████████████████████████
     500ms - 632ms   :   102 ███
     ...
```

- **Total time** — wall clock for the entire test
- **Throughput** — requests per second sustained across the full test
- **Latency** — `avg` is the mean across successful requests; `p95`/`p99` are nearest-rank percentiles and are what you should size production against; `stddev` quantifies spread
- **Status codes** — distribution of HTTP response codes (bar is relative to total successful)
- **Latency distribution** — ten linear buckets between min and max so the *shape* of the distribution (long tail, bimodality, narrow vs spread) is visible at a glance, not just summary statistics

When requests fail, an additional **Errors by class** block appears between the basic counts and the latency block, bucketing failures into `timeout`, `connection refused`, `DNS`, `TLS`, and `other` so a 100% failure rate can be diagnosed without inspecting individual errors.

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

## Multi-endpoint configs

For more than one endpoint, write a JSON config file. The format is defined by
[`schemas/v1/config.schema.json`](../schemas/v1/config.schema.json); pin the
schema URL via `$schema` and your editor handles autocomplete and validation.

```json
{
  "$schema": "https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v1/config.schema.json",
  "version": 1,
  "GET": [
    { "name": "health", "url": "https://api.example.com/health", "c": 50, "requests": 200 }
  ],
  "POST": [
    {
      "name": "login",
      "url": "https://api.example.com/auth/login",
      "headers": { "Content-Type": "application/json" },
      "body": "{\"email\":\"u@example.com\"}",
      "c": 20,
      "requests": 100,
      "timeout": "5s"
    }
  ]
}
```

```bash
rkload -config rkload.config.json
```

Endpoints run sequentially per group; each gets its own per-endpoint report
followed by an `=== Overall ===` aggregate. Worked example in
[`docs/examples/basic.config.json`](./examples/basic.config.json); versioning
policy (each `vN/` is immutable once published) in
[`schemas/README.md`](../schemas/README.md).

### Defaults

If you omit `c`, `requests`, or `timeout` on an endpoint, the runtime fills
in `10`, `100`, and `30s` respectively — same defaults as the JSON Schema, so
the editor view and runtime view of a partially-specified endpoint match.

### Validation

Configs are rejected at load time (clean error, exit 1) for: missing
`version`, version other than 1, `$schema` URL whose `vN` segment doesn't
match `version`, missing `url`, non-`http(s)` scheme, malformed `timeout`,
out-of-range `c`, name longer than 80 chars, and unknown top-level fields
(catches typos like `TRACE` or method-name case mismatches).

## Coming soon

The next versions will add:

- **v0.3.1** — `rkload import openapi <spec>` to generate configs from OpenAPI / Swagger
- **v0.3.2** — `rkload import postman <collection>` for Postman Collection v2.1
- **v0.4.0** — Multi-step scenarios with auth chains
- **v0.5.0** — `-output json` / Markdown / HTML for machine-readable results and CI integration

See [ROADMAP.md](../ROADMAP.md) for the full plan.
