// Package config parses and validates rkload's JSON configuration.
//
// The on-disk format is defined by the JSON Schema at
// schemas/v1/config.schema.json. Schemas are versioned by URL path
// (schemas/vN/) and immutable once published; see schemas/README.md.
// Including the schema's $id as the "$schema" field in a config file
// enables editor autocomplete and validation in JSON-Schema-aware
// editors.
//
// The runtime cross-checks the config's "version" integer against the
// version segment of "$schema" and rejects mismatches.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// SchemaVersion is the schema version this binary understands. Configs
// declaring any other version are rejected with a clear error so old
// binaries don't silently mis-validate newer files.
const SchemaVersion = 1

// Defaults applied to endpoints that omit the corresponding field.
// Mirror the JSON Schema defaults so the editor view and the runtime
// view of a partially-specified endpoint match.
const (
	DefaultConcurrency = 10
	DefaultRequests    = 100
	DefaultTimeout     = "30s"
)

// Config is a parsed rkload configuration.
type Config struct {
	Schema  string     `json:"$schema,omitempty"`
	Version int        `json:"version"`
	GET     []Endpoint `json:"GET,omitempty"`
	POST    []Endpoint `json:"POST,omitempty"`
	PUT     []Endpoint `json:"PUT,omitempty"`
	PATCH   []Endpoint `json:"PATCH,omitempty"`
	DELETE  []Endpoint `json:"DELETE,omitempty"`
	HEAD    []Endpoint `json:"HEAD,omitempty"`
	OPTIONS []Endpoint `json:"OPTIONS,omitempty"`
}

// Endpoint is a single HTTP target to load test.
type Endpoint struct {
	Name        string            `json:"name,omitempty"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
	Concurrency int               `json:"c,omitempty"`
	Requests    int               `json:"requests,omitempty"`
	Timeout     string            `json:"timeout,omitempty"`
}

// MethodGroup pairs an HTTP method with the endpoints declared under it.
type MethodGroup struct {
	Method    string
	Endpoints []Endpoint
}

// Groups returns the (method, endpoints) pairs in a stable iteration
// order so reports across runs of the same config are comparable.
func (c *Config) Groups() []MethodGroup {
	return []MethodGroup{
		{"GET", c.GET},
		{"POST", c.POST},
		{"PUT", c.PUT},
		{"PATCH", c.PATCH},
		{"DELETE", c.DELETE},
		{"HEAD", c.HEAD},
		{"OPTIONS", c.OPTIONS},
	}
}

// ParsedTimeout returns the endpoint's Timeout as a time.Duration.
// Validation already guarantees the string parses, so callers can
// treat a non-nil error here as a bug.
func (e *Endpoint) ParsedTimeout() (time.Duration, error) {
	return time.ParseDuration(e.Timeout)
}

// Load reads, parses, validates, and default-fills a JSON config file.
// Unknown fields are rejected (additionalProperties:false in the schema)
// so a typo like "url:" → "ulr:" surfaces immediately instead of being
// silently ignored.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: opening %s: %w", path, err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	c.ApplyDefaults()
	return c, nil
}

// Parse decodes raw JSON bytes into a Config without validating or
// applying defaults. Callers that already have the bytes in memory
// (e.g. for cache hashing) can use this to avoid a second file read.
//
// Unknown fields are still rejected — Parse is structurally strict;
// only the semantic Validate step is skipped. The returned error is
// the raw decoder error so callers can wrap it with their own
// context (path, source, etc.).
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// schemaVersionRe extracts the vN segment from a $schema URL such as
//
//	https://.../schemas/v1/config.schema.json
var schemaVersionRe = regexp.MustCompile(`/schemas/v(\d+)/`)

// Validate enforces the contract documented in the JSON Schema and
// cross-checks the version field against the $schema URL segment.
func (c *Config) Validate() error {
	if c.Version == 0 {
		return fmt.Errorf(`config: "version" is required (current schema version is %d)`, SchemaVersion)
	}
	if c.Version != SchemaVersion {
		return fmt.Errorf("config: unsupported schema version %d (this binary supports %d)", c.Version, SchemaVersion)
	}

	if c.Schema != "" {
		if m := schemaVersionRe.FindStringSubmatch(c.Schema); m != nil {
			if urlVer := m[1]; urlVer != fmt.Sprint(c.Version) {
				return fmt.Errorf(`config: $schema URL pins schema v%s but "version" field is %d (these must match — see schemas/README.md)`, urlVer, c.Version)
			}
		}
	}

	total := 0
	for _, g := range c.Groups() {
		for i, ep := range g.Endpoints {
			if err := ep.validate(g.Method, i); err != nil {
				return err
			}
			total++
		}
	}
	if total == 0 {
		return fmt.Errorf("config: no endpoints defined under any HTTP method")
	}
	return nil
}

func (e *Endpoint) validate(method string, index int) error {
	loc := fmt.Sprintf("%s[%d]", method, index)

	if e.URL == "" {
		return fmt.Errorf(`config: %s: "url" is required`, loc)
	}
	if !strings.HasPrefix(e.URL, "http://") && !strings.HasPrefix(e.URL, "https://") {
		return fmt.Errorf("config: %s: url %q must start with http:// or https://", loc, e.URL)
	}
	if e.Concurrency < 0 || e.Concurrency > 10000 {
		return fmt.Errorf("config: %s: c=%d out of range [1,10000]", loc, e.Concurrency)
	}
	if e.Requests < 0 {
		return fmt.Errorf("config: %s: requests=%d must be >= 0", loc, e.Requests)
	}
	if len(e.Name) > 80 {
		return fmt.Errorf("config: %s: name exceeds 80 characters", loc)
	}
	if e.Timeout != "" {
		if _, err := time.ParseDuration(e.Timeout); err != nil {
			return fmt.Errorf("config: %s: invalid timeout %q: %w", loc, e.Timeout, err)
		}
	}
	return nil
}

// ApplyDefaults fills zero-valued fields with their defaults so callers
// don't need to second-guess what 0/empty means. Idempotent — calling
// it twice produces the same result.
func (c *Config) ApplyDefaults() {
	for _, g := range c.Groups() {
		for i := range g.Endpoints {
			ep := &g.Endpoints[i]
			if ep.Concurrency == 0 {
				ep.Concurrency = DefaultConcurrency
			}
			if ep.Requests == 0 {
				ep.Requests = DefaultRequests
			}
			if ep.Timeout == "" {
				ep.Timeout = DefaultTimeout
			}
		}
	}
}
