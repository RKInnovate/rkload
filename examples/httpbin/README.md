# httpbin — worked example for rkload

Nine short configs that exercise the load-testing features rkload offers, against [httpbin.org](https://httpbin.org) — a public HTTP request-and-response service designed for client testing. Five are single-request examples; four are multi-step **scenarios** (v2 schema) that chain requests, extract and inject variables, assert on responses, and authenticate. Run any of them out of the box; no signup, no credentials, no setup beyond having the rkload binary on `PATH`.

This example exists so you can:

- See rkload working against the real internet in 10 seconds flat
- Understand what each report block (status histogram, latency percentiles, distribution, error class breakdown) actually looks like with controlled inputs
- Copy a config and adapt it to your own API

## Prerequisites

```bash
# From the rkload repo root:
make build                       # produces ./bin/rkload
```

## Running

Each config is independent. From the repo root:

```bash
./bin/rkload -config examples/httpbin/configs/simple.rkload.json
```

Or `cd examples/httpbin && ../../bin/rkload -config configs/simple.rkload.json` if you prefer.

## The single-request configs

### `simple.rkload.json` — hello world

Single endpoint, plain GET, default everything. Run this first to confirm rkload + your network can reach httpbin:

```bash
./bin/rkload -config examples/httpbin/configs/simple.rkload.json
```

Expected: 50 successful requests, throughput in the 5–20 req/sec range depending on your latency to httpbin.

### `latency-mix.rkload.json` — distribution histogram in action

Three endpoints with **controlled** server-side delays of 0s, 1s, and 2s. Because the delays are deterministic, the latency distribution histogram clusters into three obvious bands:

```text
Latency distribution:
     0ms - 200ms   :  ███████████████  (instant)
     0.9s - 1.1s   :  ███████████████  (1s-delay)
     1.9s - 2.1s   :  ███████████████  (2s-delay)
```

Useful when you want to *see* what a multi-modal latency distribution looks like before trying to interpret it on real traffic.

### `status-mix.rkload.json` — successful round-trips with non-200 codes

httpbin's `/status/{code}` endpoints always return the asked-for HTTP code. This config hits `/status/200`, `/status/429`, and `/status/500` — and demonstrates a subtle but important rkload behaviour:

> Status codes other than 2xx are **not** "errors" from rkload's POV. An "error" means rkload couldn't complete the round trip at all (TCP refused, TLS broken, DNS failed, timeout). A 500 response is a successful HTTP exchange; it shows up in the status histogram, but `Errors: 0` in the summary.

This distinction is load-bearing for CI: rkload's exit code is non-zero only if any *transport-level* failure happened. If you also want to fail on 5xx responses, that's threshold-based assertion territory (planned for v1.2 — see [ROADMAP.md](../../ROADMAP.md)).

### `bearer-auth.rkload.json` — header injection

Two GETs to httpbin's `/bearer` endpoint, identical except one has an `Authorization: Bearer …` header and the other doesn't. httpbin returns:

- **200** when `Authorization: Bearer <anything>` is present
- **401** when the header is missing

So the status histogram becomes a clear A/B:

```text
=== GET bearer-with-token ===
  HTTP 200: 50 ████████████████████

=== GET bearer-without-token ===
  HTTP 401: 50 ████████████████████
```

If you're load-testing a real authenticated API, this is the pattern: drop your token (Bearer, session cookie, API key, whatever) into the endpoint's `headers` object and rkload sends it on every request.

### `post-with-body.rkload.json` — POST with a JSON body

Echoes a fake "checkout" event through httpbin's `/post` endpoint. The rkload loader creates a fresh `strings.NewReader` per request, so the same body string drives every concurrent worker safely.

Pattern to copy when stress-testing your own create-resource endpoints: put the JSON payload as a string in `body`, set `Content-Type: application/json`, and rkload handles the rest.

## The scenario configs

These use the **v2 schema** (`scenarios`): each virtual user runs an ordered
chain of steps, carrying state between them. They're deliberately light on
httpbin (`vus: 2`, `iterations: 5`) — bump those for real load. Every one is
self-verifying: the assertions pass only if the feature actually worked.

### `scenario-extract.rkload.json` — extract a value and inject it

A two-step chain that proves variable flow end to end:

1. `GET /response-headers?X-Token=chain-demo-42` — httpbin echoes that value in
   its JSON body; the step `extract`s it into `${token}`.
2. `GET /anything` with `Authorization: Bearer ${token}` — httpbin echoes the
   request back, and the step asserts the body **contains `chain-demo-42`**.

Because the injected value shows up in step 2's echoed response, a green run is
proof the extract → inject path works. Swap `from: "json"` for
`from: "header"`, `"status"`, or `"regex"` to read the value a different way.

### `scenario-assert.rkload.json` — all three assertion types

One step against httpbin's fixed `/json` document, asserting `status` equals
200, `body-contains` `"slideshow"`, and two `json-equals` checks on nested
paths (`slideshow.title`, `slideshow.author`). A failed assertion aborts the
chain and flips the exit code — the pattern for turning a load test into a CI
gate.

### `scenario-bearer-auth.rkload.json` — bearer auth

A scenario-level `auth` block of `type: "bearer"` stamps
`Authorization: Bearer rkload-demo-token` on the request to `/bearer`. httpbin
echoes the token back, so the step asserts `authenticated == true` **and**
`token == rkload-demo-token`. Put a real token in `${ENV}` form
(`"token": "${API_TOKEN}"`) to keep it out of the file.

### `scenario-basic-auth.rkload.json` — basic auth

An `auth` block of `type: "basic"` with `username` / `password` base64-encodes
the credentials for `/basic-auth/rkload/s3cret`; the step asserts
`authenticated == true` and `user == rkload`. A step can also carry its own
`auth`, which overrides the scenario's for that step.

## Adapting these to your own API

The configs are intentionally short so you can copy-paste, swap URLs, and start. Five practical patterns to remember:

| You want to… | Add this to the endpoint |
|---|---|
| Send a fixed auth token | `"headers": { "Authorization": "Bearer YOUR_TOKEN" }` |
| Send a session cookie | `"headers": { "Cookie": "session=YOUR_SESSION_ID" }` |
| Stress a POST/PUT | use `POST` / `PUT` key in the config, set `body` |
| Vary concurrency per endpoint | bump `c` (workers) on the heavy ones, keep others light |
| Longer-running endpoints | raise `timeout` from `30s` default |

For a config you want to keep on disk without committing (e.g. one with a real token in it), name it `*.local.json` — that's gitignored by default.

**Run the whole directory at once** — `rkload -config examples/httpbin/configs/` loads every `*.rkload.json` file in lexical order and runs them as one combined session under a single TUI / aggregate report. The compound `.rkload.json` suffix is intentional: directory-mode ignores plain `*.json` files so an unrelated data fixture sitting next to your configs won't trip the schema validator.

## Where to go from here

- Generate a config from an existing OpenAPI / Swagger spec: `rkload import openapi <spec>`
- Generate a config from a Postman collection: `rkload import postman <collection>`
- Start from scratch with `rkload init <file>`
- Validate (and cache the validation result) with `rkload validate <file>`

See the [top-level README](../../README.md) for full CLI documentation.
