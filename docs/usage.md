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
- Consider whether you actually want to test the root or health endpoint, or your real API workflow. The latter is what [scenarios](#scenarios-multi-step-chains) are for.

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
`version`, an unsupported version, `$schema` URL whose `vN` segment doesn't
match `version`, missing `url`, non-`http(s)` scheme, malformed `timeout`,
out-of-range `c`, name longer than 80 chars, and unknown top-level fields
(catches typos like `TRACE` or method-name case mismatches).

## Scenarios (multi-step chains)

A **scenario** is an ordered chain of requests that each virtual user runs in
sequence, carrying state between steps: extract a value from one response (a
token, an id) and inject it into a later request. Scenarios need schema v2 — a
strict superset of v1, so a v2 config can still declare method-keyed endpoints
alongside its `scenarios`.

```json
{
  "$schema": "https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v2/config.schema.json",
  "version": 2,
  "scenarios": [
    {
      "name": "login-list-logout",
      "vus": 10,
      "iterations": 200,
      "timeout": "10s",
      "steps": [
        {
          "name": "login", "method": "POST",
          "url": "https://api.example.com/auth/login",
          "headers": { "Content-Type": "application/json" },
          "body": "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}",
          "extract": [ { "var": "token", "from": "json", "path": "data.accessToken" } ],
          "assert": [ { "type": "status", "equals": 200 } ]
        },
        {
          "name": "list", "method": "GET",
          "url": "https://api.example.com/projects",
          "auth": { "type": "bearer", "token": "${token}" },
          "assert": [ { "type": "status", "equals": 200 } ]
        }
      ]
    }
  ]
}
```

```bash
rkload -config scenario.config.json
```

- **`vus`** — concurrent virtual users (mirrors an endpoint's `c`);
  **`iterations`** — total chain runs across all VUs (mirrors `requests`).
  Both default to `10` / `100`.
- **`timeout`** — per-request timeout applied to every step.

### Variables: extract and inject

Each step may `extract` values from its response and bind them to variables
usable as `${name}` in a later step's URL, headers, body, or auth:

| `from`   | reads                                   | field                             |
|----------|-----------------------------------------|-----------------------------------|
| `json`   | a dotted path into the JSON body        | `path` (e.g. `data.items.0.id`)   |
| `header` | a response header                       | `name`                            |
| `status` | the numeric status code                 | —                                 |
| `regex`  | the first capture group of a body match | `pattern`                         |

`${name}` resolves against extracted variables first, then the process
environment — so `${API_TOKEN}` reads from the env while `${token}` comes from a
prior `extract`. An unresolved placeholder is left verbatim so the gap is
visible. Environment interpolation keeps secrets out of the config file.

### Assertions

Each step may `assert` conditions on its response. A failed assertion marks the
step failed, aborts the rest of that chain iteration, and drives the non-zero
exit code (load-bearing for CI):

| `type`          | passes when                | fields          |
|-----------------|----------------------------|-----------------|
| `status`        | status code equals         | `equals`        |
| `body-contains` | body contains a substring  | `value`         |
| `json-equals`   | value at a JSON path equals | `path`, `value` |

### Auth

An optional `auth` block authenticates a scenario's requests, applied to every
step unless the step provides its own (a step's explicit header still wins):

| `type`   | sets                                            |
|----------|-------------------------------------------------|
| `bearer` | `Authorization: Bearer <token>`                 |
| `apikey` | a header (default `Authorization`) to `<token>` |
| `basic`  | `Authorization: Basic base64(user:pass)`        |

Credential fields interpolate `${ENV}` placeholders, so tokens live in the
environment, not the file. `oauth2` is reserved in the schema but not yet
executed (it returns a clear error).

A worked example lives in
[`docs/examples/scenario.config.json`](./examples/scenario.config.json).

## Generating configs from API specs

`rkload import` produces a ready-to-run rkload config from an existing
API specification, so teams with OpenAPI specs don't hand-write
`rkload.config.json`.

```bash
rkload import openapi spec.yaml -o rkload.config.json
rkload import openapi -c 50 -n 1000 spec.json -o rkload.config.json
rkload import openapi --tag billing spec.yaml -o billing.config.json
rkload import openapi --path-prefix /api/v1/ spec.yaml -o v1.config.json
```

Supports OpenAPI 3.x and Swagger 2.0; both JSON and YAML are
auto-detected by inspecting the first non-whitespace byte.

### Flags

| Flag             | Default        | Description                                                              |
|------------------|----------------|--------------------------------------------------------------------------|
| `-o`             | _(stdout)_     | Output file                                                              |
| `-c`             | `0`            | Default concurrency for generated endpoints (`0` = config default `10`)  |
| `-n`             | `0`            | Default request count (`0` = config default `100`)                       |
| `-timeout`       | `""`           | Default timeout (`""` = config default `30s`)                            |
| `--tag`          | _(none)_       | Include only operations whose tags contain this value                    |
| `--path-prefix`  | _(none)_       | Include only paths starting with this prefix (e.g. `/api/v1/`)           |

> **Flag ordering:** Flags must come before the spec path. Go's stdlib
> flag parser stops at the first positional argument, so
> `rkload import openapi spec.yaml --tag x` is wrong — write
> `rkload import openapi --tag x spec.yaml` instead.

### What gets mapped

- **URL** — `servers[0].url + path` (OpenAPI 3) or
  `schemes[0]://host + basePath + path` (Swagger 2). Errors clearly
  if the spec doesn't carry enough info to construct a full URL.
- **Method** — operation's HTTP method, grouped under the matching
  schema key (`GET`, `POST`, ...).
- **Name** — `operationId` if set, otherwise
  `method-path-with-dashes`, clamped to the schema's 80-char limit.
- **Body** — JSON example pulled from
  `requestBody.content."application/json".example` (OpenAPI 3) or
  the `in:"body"` parameter's `example` / `x-example` (Swagger 2).
  Object examples are JSON-encoded; strings pass through.
- **Headers** — `Content-Type: application/json` is added when a
  body is emitted. `Authorization: REPLACE_ME` is added for any
  operation with an effective security requirement (per-op security
  overrides global; explicit `security: []` opts out).
- **Defaults** — `c`, `requests`, and `timeout` come from the CLI
  flags so the generated file is immediately runnable.

### Limitations

- **Path templates left as-is.** `/users/{id}` in the spec becomes
  `/users/{id}` in the config. Guessing a value (e.g. from a
  parameter `example`) would silently wrong-load-test the wrong
  resources, so you edit these yourself before running.
- **Auth tokens are placeholders.** Specs don't carry real
  credentials. Grep the output for `REPLACE_ME` to find every
  endpoint that needs a real header value.
- **No body schemas.** Only literal `example` / `x-example` values
  are extracted; JSON Schema-driven payload generation is out of
  scope for the importer (write the body by hand for those).

### Determinism

Re-running the importer on the same spec produces a byte-identical
output file (paths sorted lexically, methods in fixed order). This
matters when you commit the generated config and review changes
under `git diff` — the diff only shows real spec changes, not
iteration-order noise.

## Importing Postman Collections

For teams that already maintain API collections in Postman, the
v2.1 Collection format imports the same way:

```bash
rkload import postman collection.json -o rkload.config.json

# Substitute Postman {{vars}} at generation time
rkload import postman --var baseUrl=https://prod.example.com \
                     --var token=eyJhbGc...                  \
                     collection.json -o rkload.config.json
```

### Flags

| Flag             | Default        | Description                                                              |
|------------------|----------------|--------------------------------------------------------------------------|
| `-o`             | _(stdout)_     | Output file                                                              |
| `-c`             | `0`            | Default concurrency (`0` = config default `10`)                          |
| `-n`             | `0`            | Default request count (`0` = config default `100`)                       |
| `-timeout`       | `""`           | Default timeout (`""` = config default `30s`)                            |
| `--path-prefix`  | _(none)_       | Include only endpoints whose URL contains this substring                 |
| `--var`          | _(none)_       | Override a Postman `{{var}}`. Repeatable: `--var k1=v1 --var k2=v2`      |

### Mapping rules

- **Folder flattening.** Nested `item[].item[]` produces a flat
  config — folder structure is preserved only in endpoint names,
  not in the output shape.
- **Variables.** `{{var}}` references are first resolved against
  the collection's own `variable[]` array, then user `--var`
  overrides are layered on top. Unknown variables pass through
  verbatim so you can grep them and decide.
- **URL.** Postman's URL field is dual-shaped (string OR object
  with `raw`/`host`/`path`); both forms are normalised. The `raw`
  field wins when present.
- **Headers.** `header[].disabled: true` entries are dropped to
  match Postman's own send behaviour. Header values run through
  variable substitution too.
- **Body.** Only `body.mode: "raw"` is extracted (the common case
  for JSON APIs). `formdata` / `urlencoded` / `file` modes
  silently produce an empty body — write those by hand.
- **Schema version.** Only Collection v2.1 is supported; v2.0 is
  rejected with a clear error.

## Coming soon

- **v0.5.0** — `-output json` / Markdown / HTML for machine-readable results and CI integration

See [ROADMAP.md](../ROADMAP.md) for the full plan.
