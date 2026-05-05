// Package config parses and validates rkload's JSON configuration.
//
// The config format is defined by the JSON Schema at
// schemas/v1/config.schema.json. Schemas are versioned by URL path
// (schemas/vN/) and immutable once published; see schemas/README.md
// for the versioning policy. Including the schema's $id as the "$schema"
// field in a config file enables editor autocomplete and validation in
// VS Code and other JSON-Schema-aware editors. The runtime cross-checks
// the config's "version" integer against the version segment of "$schema"
// and rejects mismatches.
//
// Planned in v0.3.0:
//   - JSON config loader (schema version 1)
//   - Endpoints grouped by HTTP method, each with per-endpoint
//     concurrency, request count, headers, body, and timeout
//   - Validation with helpful error messages
//   - Environment variable interpolation ({{ env.NAME }})
//
// Planned in v0.4.0:
//   - Scenario chains with variable capture and injection
//   - Auth helper configuration
//
// This package is empty in v0.1.0.
package config
