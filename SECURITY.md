# Security Policy

## Reporting a vulnerability

If you discover a security vulnerability in rkload, please report it privately by emailing **ravindra@budgurjar.org**. Please do not open a public issue for security-sensitive reports.

We will:

- Acknowledge your report within 48 hours
- Provide an initial assessment within 7 days
- Aim to ship a fix or mitigation within 30 days for confirmed issues
- Credit you in the release notes (unless you prefer to remain anonymous)

## Scope

rkload is a load testing tool. Security concerns we care about most:

1. **Credential handling** — once scenario configs land in v0.4, secrets in YAML must be handled safely (env var interpolation, never logged in plaintext).
2. **Dependency vulnerabilities** — we monitor `go.mod` via Dependabot.
3. **Output sanitization** — generated reports must safely encode arbitrary response bodies.

## Out of scope

- Issues caused by misuse of the tool to test systems you do not own
- Vulnerabilities in third-party APIs that rkload tests
- Attacks against the rkload binary itself when run with adversarial flags

## Responsible use

rkload can generate significant traffic. Using it against systems without explicit written permission may violate computer fraud and abuse laws in your jurisdiction. The maintainers are not responsible for misuse.

Always:

- Test only systems you own or are explicitly authorized to test
- Be aware that many cloud providers consider load testing a "stress event" requiring prior notification
- Coordinate with infrastructure teams before testing shared environments
