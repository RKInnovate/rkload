# Contributing to rkload

Thank you for considering a contribution! This guide will help you get started.

## Code of conduct

This project adheres to the [Contributor Covenant](./CODE_OF_CONDUCT.md). By participating, you agree to abide by its terms.

## Development setup

**Requirements:**

- Go 1.22 or later
- Make (recommended)
- Git

**Clone and build:**

```bash
git clone https://github.com/RKInnovate/rkload.git
cd rkload
make build
./bin/rkload --help
```

**Run tests:**

```bash
make test    # full test suite with race detector
make vet     # go vet static analysis
make lint    # full linting (installs staticcheck on first run)
make fmt     # auto-format with gofmt
```

## Submitting changes

1. Fork the repository and create a topic branch from `main`:

   ```bash
   git checkout -b feat/percentile-reporting
   ```

2. Make your changes. Keep commits focused and well-described.

3. Add or update tests for any behavior change.

4. Ensure `make lint` and `make test` pass cleanly.

5. Open a pull request with a clear description of:
   - What problem the change solves
   - How it solves it
   - Any tradeoffs or alternatives considered

## Commit style

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` — new feature
- `fix:` — bug fix
- `docs:` — documentation only
- `refactor:` — internal change without feature/fix
- `test:` — test additions or fixes
- `chore:` — tooling, dependencies, build
- `perf:` — performance improvement

Example:

```
feat(report): add p50/p95/p99 latency percentiles

Sorts the duration slice and computes percentiles using simple
index lookup. Sufficient for sample sizes up to ~1M requests.

Closes #12
```

## What to work on

Check the [Issues](https://github.com/RKInnovate/rkload/issues) tab. Issues tagged `good first issue` are a good entry point.

The [ROADMAP](./ROADMAP.md) shows where the project is heading — features for the next version are usually tracked there.

## Questions

For questions that aren't bug reports, open a GitHub Discussion or reach out via the contacts in the [README](./README.md).
