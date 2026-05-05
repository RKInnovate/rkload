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

| Flag | Default | Description |
|---|---|---|
| `-url` | _(required)_ | Target URL to load test |
| `-c` | `10` | Number of concurrent workers |
| `-n` | `100` | Total number of requests |
| `-method` | `GET` | HTTP method (GET, POST, PUT, DELETE, etc.) |
| `-version` | `false` | Print version and exit |

---

## Roadmap

`rkload` is under active development. The current version is a focused MVP. See [ROADMAP.md](./ROADMAP.md) for the planned feature progression:

- **v0.2** — Latency percentiles (p50/p95/p99), error grouping, distribution histogram ✅
- **v0.3** — JSON-driven configuration, multi-endpoint suites (schema v1 sketched in [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json); see [versioning policy](./schemas/README.md))
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
