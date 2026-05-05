# rkload schemas

Published JSON Schemas that describe rkload file formats. Editors and CI tools
consume these via the `$schema` field in user files.

## Versioning policy

Each schema version lives at its own immutable path:

```
schemas/
  v1/config.schema.json   # schema version 1 (current)
  v2/config.schema.json   # added when a breaking change is needed
  ...
```

**Rules:**

1. A published schema file is **never modified in place**. Once `v1/config.schema.json`
   ships, it is frozen forever — only typos in `description` text may be touched,
   never the validation rules. Any rule change ships as a new `vN/` directory.
2. User config files MUST pin a versioned `$schema` URL
   (`.../schemas/v1/config.schema.json`, never `.../schemas/config.schema.json`).
   There is intentionally no "latest" alias.
3. The top-level `version` integer in a config (`{"version": 1, ...}`) MUST match
   the version segment of its `$schema` URL. The runtime cross-checks both and
   rejects mismatches with a clear error.
4. The runtime supports a documented set of versions at any given time; dropping
   support for an old version is a major release and must appear in CHANGELOG.

## Why URL-based versioning

Pointing every config at a single mutable URL would mean a future v2 silently
re-validates every v1 config in the wild — possibly incorrectly, possibly
catastrophically. Versioned URLs give each config a stable, immutable contract
for as long as the file exists.

## Current versions

| Version | Path                          | Status  | Introduced |
| ------- | ----------------------------- | ------- | ---------- |
| 1       | `v1/config.schema.json`       | current | v0.3.0     |
