# rkload

> A scenario-driven HTTP load testing tool, built for real production validation.

[![CI](https://github.com/RKInnovate/rkload/actions/workflows/ci.yml/badge.svg)](https://github.com/RKInnovate/rkload/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/RKInnovate/rkload)](https://goreportcard.com/report/github.com/RKInnovate/rkload)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/RKInnovate/rkload)](./go.mod)

`rkload` is a fast, configurable HTTP load testing tool for engineers who need real answers before going to production. Unlike generic URL pingers, it is designed around realistic workflows — concurrent goroutine-based load generation, percentile-aware reporting, and (soon) multi-step scenarios with auth flows.

Built in Go. Single binary, zero runtime dependencies.

---

## Why another load tester?

Existing tools are either too generic (single endpoint, no flow control) or too heavy (full scripting environments for simple use cases). `rkload` sits in the middle: a small, opinionated tool that fits naturally into a CI pipeline and grows with your needs.

It is built and used by the team at [RK Innovate](https://github.com/RKInnovate) to validate production deployments.

---

## Quick start

Install:

```bash
go install github.com/RKInnovate/rkload/cmd/rkload@latest
```

Or build from source:

```bash
git clone https://github.com/RKInnovate/rkload.git
cd rkload
make build
./bin/rkload --help
```

Run a basic load test:

```bash
rkload -url https://api.example.com/health -c 50 -n 1000
```

Output:

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
     632ms - 764ms   :    34 █
     764ms - 896ms   :    14
     896ms - 1.028s  :     8
    1.028s - 1.16s   :     5
     1.16s - 1.292s  :     7
    1.292s - 1.424s  :     3
    1.424s - 1.556s  :     2
    1.556s - 1.688s  :     2
```

---

## Flags

| Flag       | Default       | Description                                                          |
|------------|---------------|----------------------------------------------------------------------|
| `-url`     | _(required¹)_ | Target URL to load test (single-endpoint mode)                       |
| `-config`  | _(required¹)_ | Path to a JSON config file (multi-endpoint mode, see below)          |
| `-c`       | `10`          | Number of concurrent workers (single-endpoint mode)                  |
| `-n`       | `100`         | Total number of requests (single-endpoint mode)                      |
| `-method`  | `GET`         | HTTP method (single-endpoint mode)                                   |
| `-version` | `false`       | Print version and exit                                               |

¹ Exactly one of `-url` or `-config` is required.

---

## Multi-endpoint configs

For testing more than one endpoint in a single run, use a JSON config file. The format is defined by the JSON Schema at [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json) — pin the schema URL via `$schema` and your editor will give you autocomplete and validation.

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
      "body": "{\"email\":\"u@example.com\",\"password\":\"…\"}",
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

Endpoints run **sequentially per group** so each gets its own clean per-endpoint report (latency, error breakdown, distribution histogram), followed by an `=== Overall ===` aggregate. The exit code is non-zero if any endpoint had any failed request — same CI semantic as the single-URL mode.

See [`docs/examples/basic.config.json`](./docs/examples/basic.config.json) for a fuller example and [`schemas/README.md`](./schemas/README.md) for the schema versioning policy (each `vN/` is immutable once published — never modify a published schema in place).

---

## Generating configs from existing API specs

If you already have an OpenAPI 3.x, Swagger 2.0, or (next release) Postman Collection, `rkload import` produces a ready-to-run config:

```bash
# OpenAPI 3.x or Swagger 2.0 (JSON or YAML, auto-detected)
rkload import openapi spec.yaml -o rkload.config.json

# Filter to a single tag or path subtree
rkload import openapi --tag billing spec.yaml -o billing.config.json
rkload import openapi --path-prefix /api/v1/ spec.yaml -o v1.config.json

# Override per-endpoint defaults at generation time
rkload import openapi -c 50 -n 1000 -timeout 10s spec.yaml -o rkload.config.json
```

What you get:

- Each operation becomes one endpoint, grouped by HTTP method
- `operationId` is the endpoint's `name` (falls back to `method-path`)
- `requestBody.example` (OpenAPI 3) and `parameters[in=body].example` / `x-example` (Swagger 2) become the request `body`
- Operations with security requirements get `Authorization: REPLACE_ME` placeholders — grep for `REPLACE_ME` to find every endpoint that needs a real token before running
- Path templates like `/users/{id}` are emitted verbatim — substituting them would mean guessing values, so you edit them yourself

Output is deterministic — re-running the importer on the same spec produces a byte-identical file, so generated configs are review-friendly under `git diff`.

> **Flag ordering:** Go's stdlib flag parser stops at the first positional, so flags must come before the spec path: `rkload import openapi --tag x spec.yaml`, not `rkload import openapi spec.yaml --tag x`.

---

## Roadmap

`rkload` is under active development. The current version is a focused MVP. See [ROADMAP.md](./ROADMAP.md) for the planned feature progression:

- **v0.2** — Latency percentiles (p50/p95/p99), error grouping, distribution histogram ✅
- **v0.3** — JSON-driven configuration, multi-endpoint suites ✅ (schema v1 in [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json); see [versioning policy](./schemas/README.md))
- **v0.3.1** — `rkload import openapi <spec>` to generate configs from OpenAPI / Swagger ✅
- **v0.3.2** — `rkload import postman <collection>` for Postman Collection v2.1
- **v0.4** — Scenario chains, auth helpers, variable extraction
- **v0.5** — JSON / Markdown / HTML reporting, CI integration
- **v1.0** — Stable, documented, production-ready

---

## Documentation

- [Usage Guide](./docs/usage.md) — flags, interpreting results, choosing concurrency
- [Architecture](./docs/architecture.md) — how the engine works under the hood
- [Roadmap](./ROADMAP.md) — what's planned and when
- [Contributing](./CONTRIBUTING.md) — how to help

---

## Responsible use

`rkload` can generate significant traffic. Only test systems you own or have explicit written permission to test. Misuse may violate computer fraud and abuse laws in your jurisdiction. See [SECURITY.md](./SECURITY.md) for more.

---

## License

MIT — see [LICENSE](./LICENSE).

Maintained by [Ravindra Singh Budgurjar](https://github.com/badrat-in) and the [RK Innovate](https://github.com/RKInnovate) team.
