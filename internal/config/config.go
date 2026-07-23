// Package config parses and validates rkload's JSON configuration.
//
// The on-disk format is defined by the JSON Schemas under schemas/vN/.
// Schemas are versioned by URL path and immutable once published; see
// schemas/README.md. Including the schema's $id as the "$schema" field
// in a config file enables editor autocomplete and validation in
// JSON-Schema-aware editors.
//
// The runtime cross-checks the config's "version" integer against the
// version segment of "$schema" and rejects mismatches. Schema v1 is a
// flat map of HTTP-method → endpoints; schema v2 is a strict superset
// that adds a top-level "scenarios" array of ordered request chains.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the schema version that rkload's own tooling
// (`rkload init`, the spec importers) emits and the baseline this binary
// defaults to. The set of versions this binary can *validate* is
// SupportedSchemaVersions — a config declaring anything outside that set
// is rejected with a clear error so old binaries don't silently
// mis-validate newer files.
const SchemaVersion = 1

// SupportedSchemaVersions is the set of "version" integers this binary
// can load. Schema versions are additive and immutable (schemas/vN/);
// dropping one is a breaking change (see schemas/README.md). v2 adds the
// top-level "scenarios" array as a strict superset of v1.
var SupportedSchemaVersions = map[int]bool{1: true, 2: true}

// Defaults applied to endpoints (and scenarios) that omit the
// corresponding field. Mirror the JSON Schema defaults so the editor
// view and the runtime view of a partially-specified config match.
const (
	DefaultConcurrency = 10
	DefaultRequests    = 100
	DefaultTimeout     = "30s"
)

// Config is a parsed rkload configuration.
type Config struct {
	Schema    string     `json:"$schema,omitempty"`
	Version   int        `json:"version"`
	GET       []Endpoint `json:"GET,omitempty"`
	POST      []Endpoint `json:"POST,omitempty"`
	PUT       []Endpoint `json:"PUT,omitempty"`
	PATCH     []Endpoint `json:"PATCH,omitempty"`
	DELETE    []Endpoint `json:"DELETE,omitempty"`
	HEAD      []Endpoint `json:"HEAD,omitempty"`
	OPTIONS   []Endpoint `json:"OPTIONS,omitempty"`
	Scenarios []Scenario `json:"scenarios,omitempty"` // schema v2+
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

// Scenario is an ordered chain of HTTP steps run by each virtual user,
// carrying variable state (extracted from one step's response, injected
// as ${var} into a later step's request) across the chain. Introduced in
// schema v2.
type Scenario struct {
	Name       string `json:"name,omitempty"`
	VUs        int    `json:"vus,omitempty"`        // concurrent virtual users (mirrors endpoint "c")
	Iterations int    `json:"iterations,omitempty"` // total chain runs across all VUs (mirrors "requests")
	Timeout    string `json:"timeout,omitempty"`    // per-request timeout applied to every step
	Auth       *Auth  `json:"auth,omitempty"`       // applied to every step unless a step overrides it
	Steps      []Step `json:"steps"`
}

// Step is one request in a Scenario chain.
type Step struct {
	Name    string            `json:"name,omitempty"`
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Auth    *Auth             `json:"auth,omitempty"` // overrides the scenario-level auth for this step
	Extract []ExtractRule     `json:"extract,omitempty"`
	Assert  []AssertRule      `json:"assert,omitempty"`
}

// ExtractRule pulls a value out of a step's response and binds it to a
// variable usable as ${var} in later steps. Stdlib-only sources: a
// dotted JSON path, a response header, the status code, or a regexp
// capture group.
type ExtractRule struct {
	Var     string `json:"var"`
	From    string `json:"from"`              // json | header | status | regex
	Path    string `json:"path,omitempty"`    // from=json: dotted path, e.g. data.items.0.id
	Name    string `json:"name,omitempty"`    // from=header: header name
	Pattern string `json:"pattern,omitempty"` // from=regex: first capture group is bound
}

// AssertRule checks a step's response; a failed assertion marks the step
// as failed and aborts the rest of that chain iteration.
type AssertRule struct {
	Type   string `json:"type"`             // status | body-contains | json-equals
	Equals int    `json:"equals,omitempty"` // type=status: expected status code
	Value  string `json:"value,omitempty"`  // type=body-contains / json-equals: expected substring / value
	Path   string `json:"path,omitempty"`   // type=json-equals: dotted JSON path
}

// Auth describes how to authenticate a scenario's requests. The oauth2
// type's fields are part of the frozen v2 schema, but its execution is
// not yet implemented (the loader reports a clear error) — see ROADMAP.md.
type Auth struct {
	Type         string   `json:"type"`                   // bearer | apikey | basic | oauth2
	Token        string   `json:"token,omitempty"`        // bearer / apikey value
	Header       string   `json:"header,omitempty"`       // apikey: header name (default Authorization)
	Username     string   `json:"username,omitempty"`     // basic
	Password     string   `json:"password,omitempty"`     // basic
	ClientID     string   `json:"clientId,omitempty"`     // oauth2
	ClientSecret string   `json:"clientSecret,omitempty"` // oauth2
	TokenURL     string   `json:"tokenUrl,omitempty"`     // oauth2
	Scopes       []string `json:"scopes,omitempty"`       // oauth2
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
// Validation already guarantees the string parses, so callers can treat
// a non-nil error here as a bug.
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
// Unknown fields are still rejected — Parse is structurally strict; only
// the semantic Validate step is skipped. The returned error is the raw
// decoder error so callers can wrap it with their own context.
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

// supportedVersionsList renders SupportedSchemaVersions as a sorted,
// comma-separated string for error messages (e.g. "1, 2").
func supportedVersionsList() string {
	vs := make([]int, 0, len(SupportedSchemaVersions))
	for v := range SupportedSchemaVersions {
		vs = append(vs, v)
	}
	sort.Ints(vs)
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}

// Validate enforces the contract documented in the JSON Schema and
// cross-checks the version field against the $schema URL segment. The
// cross-check runs for every version so a URL/field mismatch surfaces
// regardless of which version's rules apply below.
func (c *Config) Validate() error {
	if c.Version == 0 {
		return fmt.Errorf(`config: "version" is required (current schema version is %d)`, SchemaVersion)
	}
	if !SupportedSchemaVersions[c.Version] {
		return fmt.Errorf("config: unsupported schema version %d (this binary supports %s)", c.Version, supportedVersionsList())
	}

	if c.Schema != "" {
		if m := schemaVersionRe.FindStringSubmatch(c.Schema); m != nil {
			if urlVer := m[1]; urlVer != fmt.Sprint(c.Version) {
				return fmt.Errorf(`config: $schema URL pins schema v%s but "version" field is %d (these must match — see schemas/README.md)`, urlVer, c.Version)
			}
		}
	}

	// scenarios are a v2 addition; reject them under a v1 declaration so
	// the mismatch surfaces instead of being silently ignored.
	if c.Version < 2 && len(c.Scenarios) > 0 {
		return fmt.Errorf(`config: "scenarios" requires schema version 2 (this config declares version %d)`, c.Version)
	}

	endpointTotal, err := c.validateEndpoints()
	if err != nil {
		return err
	}

	if endpointTotal == 0 && len(c.Scenarios) == 0 {
		if c.Version >= 2 {
			return fmt.Errorf("config: no endpoints or scenarios defined")
		}
		return fmt.Errorf("config: no endpoints defined under any HTTP method")
	}
	return nil
}

// validateEndpoints validates every endpoint in method-group order and
// returns the total count. Shared by all schema versions.
func (c *Config) validateEndpoints() (int, error) {
	total := 0
	for _, g := range c.Groups() {
		for i, ep := range g.Endpoints {
			if err := ep.validate(g.Method, i); err != nil {
				return 0, err
			}
			total++
		}
	}
	return total, nil
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
// don't need to second-guess what 0/empty means. Idempotent — calling it
// twice produces the same result.
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
