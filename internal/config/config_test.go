package config

import (
	"strings"
	"testing"
)

// ---- Load (round-trip from testdata) -------------------------------------

func TestLoad_Valid(t *testing.T) {
	c, err := Load("testdata/valid.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Version != 1 {
		t.Errorf("Version = %d, want 1", c.Version)
	}
	if len(c.GET) != 2 || len(c.POST) != 1 {
		t.Fatalf("groups: GET=%d POST=%d, want 2/1", len(c.GET), len(c.POST))
	}

	// Defaults applied to the second GET endpoint (specifies only url).
	bare := c.GET[1]
	if bare.Concurrency != DefaultConcurrency {
		t.Errorf("default c = %d, want %d", bare.Concurrency, DefaultConcurrency)
	}
	if bare.Requests != DefaultRequests {
		t.Errorf("default requests = %d, want %d", bare.Requests, DefaultRequests)
	}
	if bare.Timeout != DefaultTimeout {
		t.Errorf("default timeout = %q, want %q", bare.Timeout, DefaultTimeout)
	}

	// Explicit values left alone.
	first := c.GET[0]
	if first.Concurrency != 50 || first.Requests != 200 {
		t.Errorf("explicit values overwritten: c=%d requests=%d", first.Concurrency, first.Requests)
	}

	// POST headers + body preserved.
	post := c.POST[0]
	if post.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type header lost: %v", post.Headers)
	}
	if !strings.Contains(post.Body, "u@example.com") {
		t.Errorf("body lost: %q", post.Body)
	}
}

func TestLoad_VersionMismatchAgainstSchemaURL(t *testing.T) {
	_, err := Load("testdata/version_mismatch.json")
	if err == nil {
		t.Fatal("expected version-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "$schema URL pins schema v2") {
		t.Errorf("error message should explain the mismatch, got: %v", err)
	}
}

func TestLoad_RejectsUnknownFields(t *testing.T) {
	// "TRACE" is not in the schema's allowed top-level methods.
	_, err := Load("testdata/unknown_field.json")
	if err == nil {
		t.Fatal("expected unknown-field error, got nil")
	}
	if !strings.Contains(err.Error(), "TRACE") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("testdata/does-not-exist.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// ---- Validate (focused unit tests) ---------------------------------------

func TestValidate_VersionRequired(t *testing.T) {
	c := &Config{
		GET: []Endpoint{{URL: "https://example.com/"}},
	}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("want version-required error, got %v", err)
	}
}

func TestValidate_UnsupportedVersion(t *testing.T) {
	c := &Config{Version: 99, GET: []Endpoint{{URL: "https://example.com/"}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported schema version 99") {
		t.Errorf("want unsupported-version error, got %v", err)
	}
}

func TestValidate_NoEndpoints(t *testing.T) {
	c := &Config{Version: 1}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "no endpoints") {
		t.Errorf("want no-endpoints error, got %v", err)
	}
}

func TestValidate_EndpointURLRequired(t *testing.T) {
	c := &Config{Version: 1, GET: []Endpoint{{Name: "anon"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `"url" is required`) {
		t.Errorf("want url-required error, got %v", err)
	}
	if !strings.Contains(err.Error(), "GET[0]") {
		t.Errorf("error should include location GET[0], got %v", err)
	}
}

func TestValidate_URLScheme(t *testing.T) {
	c := &Config{Version: 1, GET: []Endpoint{{URL: "ftp://example.com/"}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "http://") {
		t.Errorf("want url-scheme error, got %v", err)
	}
}

func TestValidate_ConcurrencyOutOfRange(t *testing.T) {
	c := &Config{Version: 1, GET: []Endpoint{{URL: "https://example.com/", Concurrency: 100000}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("want concurrency-range error, got %v", err)
	}
}

func TestValidate_TimeoutMustParse(t *testing.T) {
	c := &Config{Version: 1, GET: []Endpoint{{URL: "https://example.com/", Timeout: "ten seconds"}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("want invalid-timeout error, got %v", err)
	}
}

func TestValidate_NameLengthLimit(t *testing.T) {
	long := strings.Repeat("a", 81)
	c := &Config{Version: 1, GET: []Endpoint{{URL: "https://example.com/", Name: long}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "name exceeds") {
		t.Errorf("want name-length error, got %v", err)
	}
}

// ---- Groups stable order -------------------------------------------------

func TestGroups_StableOrder(t *testing.T) {
	c := &Config{
		Version: 1,
		GET:     []Endpoint{{URL: "https://example.com/g"}},
		POST:    []Endpoint{{URL: "https://example.com/p"}},
		DELETE:  []Endpoint{{URL: "https://example.com/d"}},
	}
	wantOrder := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	got := c.Groups()
	for i, g := range got {
		if g.Method != wantOrder[i] {
			t.Errorf("Groups()[%d].Method = %q, want %q", i, g.Method, wantOrder[i])
		}
	}
}

// ---- Parse (bytes-only path) ---------------------------------------------

func TestParse_DoesNotValidate(t *testing.T) {
	// Missing "version" would fail Validate, but Parse should accept it.
	raw := []byte(`{"GET":[{"url":"https://example.com/"}]}`)
	c, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.Version != 0 {
		t.Errorf("Version = %d, want 0 (Parse should not synthesize one)", c.Version)
	}
	if len(c.GET) != 1 {
		t.Errorf("GET length = %d, want 1", len(c.GET))
	}
}

func TestParse_DoesNotApplyDefaults(t *testing.T) {
	raw := []byte(`{"version":1,"GET":[{"url":"https://example.com/"}]}`)
	c, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if c.GET[0].Concurrency != 0 {
		t.Errorf("Concurrency = %d, want 0 (Parse should leave defaults to ApplyDefaults)", c.GET[0].Concurrency)
	}
	if c.GET[0].Timeout != "" {
		t.Errorf("Timeout = %q, want empty", c.GET[0].Timeout)
	}
}

func TestParse_RejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"version":1,"GET":[{"url":"https://example.com/","banana":true}]}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
	if !strings.Contains(err.Error(), "banana") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}

func TestParse_RejectsMalformedJSON(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,`))
	if err == nil {
		t.Fatal("expected JSON syntax error")
	}
}

func TestParse_DoesNotWrapPath(t *testing.T) {
	// Parse error messages should not include "parsing <path>" — that
	// wrapping belongs to Load, which has the path; Parse callers do
	// their own wrapping.
	_, err := Parse([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "config: parsing") {
		t.Errorf("Parse should not prepend 'config: parsing' wrapper, got: %v", err)
	}
}

// ---- ApplyDefaults (called directly, not via Load) ----------------------

func TestApplyDefaults_FillsZeroFields(t *testing.T) {
	c := &Config{
		Version: 1,
		GET:     []Endpoint{{URL: "https://example.com/"}},
	}
	c.ApplyDefaults()
	g := c.GET[0]
	if g.Concurrency != DefaultConcurrency {
		t.Errorf("Concurrency = %d, want %d", g.Concurrency, DefaultConcurrency)
	}
	if g.Requests != DefaultRequests {
		t.Errorf("Requests = %d, want %d", g.Requests, DefaultRequests)
	}
	if g.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %q, want %q", g.Timeout, DefaultTimeout)
	}
}

func TestApplyDefaults_AcrossAllGroups(t *testing.T) {
	c := &Config{
		Version: 1,
		GET:     []Endpoint{{URL: "https://example.com/g"}},
		POST:    []Endpoint{{URL: "https://example.com/p"}},
		DELETE:  []Endpoint{{URL: "https://example.com/d"}},
	}
	c.ApplyDefaults()
	for _, g := range c.Groups() {
		for i, ep := range g.Endpoints {
			if ep.Concurrency != DefaultConcurrency {
				t.Errorf("%s[%d].Concurrency = %d, want %d", g.Method, i, ep.Concurrency, DefaultConcurrency)
			}
		}
	}
}

// ---- defaults idempotency ------------------------------------------------

func TestApplyDefaults_DoesNotOverrideExplicitValues(t *testing.T) {
	c := &Config{
		Version: 1,
		GET:     []Endpoint{{URL: "https://example.com/", Concurrency: 1, Requests: 1, Timeout: "1s"}},
	}
	c.ApplyDefaults()
	c.ApplyDefaults() // idempotent
	g := c.GET[0]
	if g.Concurrency != 1 || g.Requests != 1 || g.Timeout != "1s" {
		t.Errorf("explicit values overwritten by defaults: %+v", g)
	}
}
