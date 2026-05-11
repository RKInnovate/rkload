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

Install the latest release:

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/RKInnovate/rkload/main/scripts/install.sh | sh

# Windows (PowerShell)
iwr https://raw.githubusercontent.com/RKInnovate/rkload/main/scripts/install.ps1 -UseBasicParsing | iex
```

The installers download the matching binary from [GitHub Releases](https://github.com/RKInnovate/rkload/releases), place it on your `PATH` (`/usr/local/bin`, `~/.local/bin`, or `%LOCALAPPDATA%\rkload\bin`), and verify with `rkload -version`. Both scripts accept a `--version` / `-Version` flag to pin a specific tag and a `--dir` / `-Dir` flag to install elsewhere.

If you have Go on your machine you can also `go install`:

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

Once installed, `rkload update` keeps you on the latest release (see [Updating](#updating-rkload) below).

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

Subcommands: `rkload import {openapi|postman} <file>` (see [generating configs](#generating-configs-from-existing-api-specs)), `rkload validate <file>` (see [validating configs](#validating-configs)).

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

### Starting from scratch

If you don't have an OpenAPI spec or Postman collection to import from, generate a starter config and edit from there:

```bash
rkload init rkload.config.json      # write a starter to disk
rkload init                          # or print to stdout (pipe / redirect as you like)
rkload init --force rkload.config.json  # overwrite an existing file
```

The template includes one GET (using defaults), one POST (showing headers, body, timeout, and explicit `c` / `requests`) so every common knob is visible without consulting the schema. `REPLACE_ME` placeholders mark values that need real credentials before running — `grep REPLACE_ME` lets you find them all.

---

## Validating configs

For large generated configs, you don't want to re-run schema validation on every load test. `rkload validate <config>` checks a config against the schema and records the outcome in a small on-disk cache so subsequent `rkload -config` runs can skip re-validation when the file is unchanged.

```bash
rkload validate rkload.config.json
```

```text
Validated: /abs/path/rkload.config.json
  Status:    valid
  Hash:      bc9a0403…6617b4d
  Size:      559 bytes
  Schema:    https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v1/config.schema.json
  Version:   1
  Endpoints: GET=2, POST=1 (total: 3)
  Cached:    yes (~/.rkload/cache/bc9a0403…6617b4d.json)
```

The cache key is a **canonical JSON hash** — reformatting or re-ordering keys leaves the hash unchanged; any semantic edit changes it and forces re-validation on the next run. Each cache entry records the file path, size, schema URL and version, per-method endpoint counts, the rkload version that performed the validation, and a timestamp.

```bash
# Skip both reading and writing the cache (useful in CI):
rkload validate --no-cache rkload.config.json

# Redirect the cache for testing or per-project isolation:
RKLOAD_CACHE_DIR=./.rkload-cache rkload validate rkload.config.json
```

Validation runs automatically on every `rkload -config` invocation too — `validate` is just the standalone form. On a cache hit the run shows:

```text
Loaded config: rkload.config.json (schema v1)
Validation:    cached 2026-05-11 14:32 UTC (rkload 0.3.3)
```

On a cache miss (or after editing the config) it shows `re-checked and cached`. Cache write failures are reported inline and do **not** fail the run — the load test still proceeds with a freshly validated config.

---

## Generating configs from existing API specs

If you already have an OpenAPI 3.x, Swagger 2.0, or (next release) Postman Collection, `rkload import` produces a ready-to-run config:

```bash
# OpenAPI 3.x or Swagger 2.0 (JSON or YAML, auto-detected)
rkload import openapi spec.yaml -o rkload.config.json

# Postman Collection v2.1
rkload import postman collection.json -o rkload.config.json

# Substitute Postman {{vars}} at generation time (repeatable flag)
rkload import postman --var baseUrl=https://prod.example.com --var token=t collection.json -o rkload.config.json

# Filter to a single tag or path subtree
rkload import openapi --tag billing spec.yaml -o billing.config.json
rkload import openapi --path-prefix /api/v1/ spec.yaml -o v1.config.json

# Override per-endpoint defaults at generation time
rkload import openapi -c 50 -n 1000 -timeout 10s spec.yaml -o rkload.config.json

# Point endpoints at production instead of the spec's first server
rkload import openapi --server-url https://api.example.com spec.yaml -o prod.config.json
rkload import openapi --server-index 1 spec.yaml -o prod.config.json   # OpenAPI 3 only
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

## Updating rkload

`rkload update` checks GitHub Releases for a newer version, downloads the host-matching GoReleaser archive, **verifies its SHA-256 against the published `checksums.txt`**, and atomically replaces the running binary.

```bash
rkload update                     # check + install if newer
rkload update --check             # only report, don't install
rkload update --version v1.0.0    # pin or downgrade
rkload update --force             # reinstall even when already current
```

Output is the four standard lines (target → download → install → done) so you can see exactly what happened. Failures don't leave a half-swapped binary: extraction errors clean up the temp file, and rename errors roll back. On Windows the running `rkload.exe` is moved aside to `rkload.exe.old` and removed on the next run (Windows refuses to overwrite a running executable, but allows renaming it).

### Daily background check

The first time you run any `rkload` command after 24h, the binary briefly checks for a newer release and prints a one-line notice to stderr before the run starts:

```text
[update available] rkload v1.1.0 — run `rkload update` to upgrade
```

The notice is **silent on every failure** — a network blip never stands between you and your command. It's skipped automatically when:

- `version == "dev"` / `unknown` (locally built — nothing to point at)
- `RKLOAD_NO_UPDATE_CHECK=1` (explicit opt-out)
- stdout is not a tty (CI / piped / redirected — would corrupt machine-readable output)

State lives at `~/.rkload/update.json`. Within 24h of a check, the cached "latest version seen" drives the notice without any network call, so the second `rkload` of the day is free.

---

## Roadmap

`rkload` is under active development. See [ROADMAP.md](./ROADMAP.md) for the full feature progression:

- **v1.0.0** — Engine, percentiles, multi-endpoint JSON configs (schema v1 in [`schemas/v1/config.schema.json`](./schemas/v1/config.schema.json); see [versioning policy](./schemas/README.md)), OpenAPI / Swagger / Postman import, `rkload init`, `rkload validate` with content-hash cache, `rkload update` for in-place upgrades, daily background "update available" notice ✅
- **v1.1** — Scenario chains, auth helpers, variable extraction
- **v1.2** — JSON / Markdown / HTML reporting, CI integration
- **v2.0** — Reserved for breaking changes only (CLI / schema v1 are stable from v1.0)

---

## Documentation

- [Usage Guide](./docs/usage.md) — flags, interpreting results, choosing concurrency
- [Architecture](./docs/architecture.md) — how the engine works under the hood
- [Roadmap](./ROADMAP.md) — what's planned and when
- [Contributing](./CONTRIBUTING.md) — how to help

---

## Development

Common Make targets — these are the same commands CI runs, so passing them locally means CI will too:

```bash
make build              # build to ./bin/rkload
make test               # go test -v -race -coverprofile=coverage.out ./...
make vet                # go vet ./...
make lint               # vet + staticcheck (auto-installs staticcheck on first run)
make fmt                # gofmt -s -w .
make run ARGS='-url https://example.com -c 10 -n 100'
make release-snapshot   # local goreleaser dry-run
```

Run a single test:

```bash
go test -run TestRun_BoundsConcurrency ./internal/loader
go test -run TestPostman ./internal/importer
```

Requirements: Go 1.22 or later, `make`. `staticcheck` and `goreleaser` are optional and auto-installed by their respective targets.

---

## Responsible use

`rkload` can generate significant traffic. Only test systems you own or have explicit written permission to test. Misuse may violate computer fraud and abuse laws in your jurisdiction. See [SECURITY.md](./SECURITY.md) for more.

---

## License

MIT — see [LICENSE](./LICENSE).

Maintained by [Ravindra Singh Budgurjar](https://github.com/badrat-in) and the [RK Innovate](https://github.com/RKInnovate) team.
