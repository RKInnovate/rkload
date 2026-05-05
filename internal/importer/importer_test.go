package importer

import (
	"os"
	"strings"
	"testing"

	"github.com/RKInnovate/rkload/internal/config"
)

// loadFixture reads a fixture and runs OpenAPI() with default options.
// Tests that need bespoke options call OpenAPI() directly.
func loadFixture(t *testing.T, path string) *config.Config {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	cfg, err := OpenAPI(f, OpenAPIOptions{})
	if err != nil {
		t.Fatalf("OpenAPI(%s): %v", path, err)
	}
	return cfg
}

// ---- OpenAPI 3.x ---------------------------------------------------------

func TestOpenAPI3_Petstore(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.openapi.json")

	if cfg.Schema != SchemaURL {
		t.Errorf("Schema = %q, want %q", cfg.Schema, SchemaURL)
	}
	if cfg.Version != config.SchemaVersion {
		t.Errorf("Version = %d, want %d", cfg.Version, config.SchemaVersion)
	}

	// 3 GETs (admin/stats, pets, pets/{petId} — sorted lexically), 1 POST.
	if len(cfg.GET) != 3 {
		t.Errorf("GET count = %d, want 3", len(cfg.GET))
	}
	if len(cfg.POST) != 1 {
		t.Errorf("POST count = %d, want 1", len(cfg.POST))
	}

	// Lexical path ordering: /admin/stats < /pets < /pets/{petId}.
	wantOrder := []string{
		"https://petstore.example.com/v1/admin/stats",
		"https://petstore.example.com/v1/pets",
		"https://petstore.example.com/v1/pets/{petId}",
	}
	for i, ep := range cfg.GET {
		if ep.URL != wantOrder[i] {
			t.Errorf("GET[%d].URL = %q, want %q", i, ep.URL, wantOrder[i])
		}
	}
}

func TestOpenAPI3_OperationIDBecomesName(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.openapi.json")

	want := map[string]bool{"listPets": true, "getPet": true, "adminStats": true}
	for _, ep := range cfg.GET {
		if !want[ep.Name] {
			t.Errorf("unexpected GET endpoint name %q", ep.Name)
		}
	}
}

func TestOpenAPI3_GlobalSecurityEmitsAuthHeader(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.openapi.json")

	// Global security applies to listPets (no per-op override) and createPet,
	// but getPet has security:[] which explicitly opts out.
	for _, ep := range cfg.GET {
		switch ep.Name {
		case "getPet":
			if got := ep.Headers["Authorization"]; got != "" {
				t.Errorf("getPet should opt out of auth (security:[]), got %q", got)
			}
		case "listPets", "adminStats":
			if got := ep.Headers["Authorization"]; got != AuthPlaceholder {
				t.Errorf("%s.Authorization = %q, want %q (global security)", ep.Name, got, AuthPlaceholder)
			}
		}
	}
}

func TestOpenAPI3_RequestBodyExampleBecomesBody(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.openapi.json")
	post := cfg.POST[0]
	if post.Name != "createPet" {
		t.Fatalf("POST[0].Name = %q, want createPet", post.Name)
	}
	if !strings.Contains(post.Body, `"name":"Fluffy"`) {
		t.Errorf("body = %q, want JSON containing name=Fluffy", post.Body)
	}
	if got := post.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestOpenAPI3_DefaultsApplied(t *testing.T) {
	f, _ := os.Open("testdata/petstore.openapi.json")
	defer f.Close()
	cfg, err := OpenAPI(f, OpenAPIOptions{
		DefaultConcurrency: 25,
		DefaultRequests:    300,
		DefaultTimeout:     "10s",
	})
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	for _, ep := range cfg.GET {
		if ep.Concurrency != 25 || ep.Requests != 300 || ep.Timeout != "10s" {
			t.Errorf("defaults not applied to %s: %+v", ep.Name, ep)
		}
	}
}

func TestOpenAPI3_NoServersIsAnError(t *testing.T) {
	spec := `{"openapi":"3.0.3","paths":{"/x":{"get":{}}}}`
	_, err := OpenAPI(strings.NewReader(spec), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "no servers[]") {
		t.Errorf("want no-servers error, got %v", err)
	}
}

func TestOpenAPI3_DeterministicOutput(t *testing.T) {
	// Two passes over the same spec must produce the exact same Config —
	// this is what makes the import-then-edit workflow safe.
	a := loadFixture(t, "testdata/petstore.openapi.json")
	b := loadFixture(t, "testdata/petstore.openapi.json")

	if len(a.GET) != len(b.GET) || len(a.POST) != len(b.POST) {
		t.Fatal("group sizes differ between runs")
	}
	for i := range a.GET {
		if a.GET[i].URL != b.GET[i].URL || a.GET[i].Name != b.GET[i].Name {
			t.Errorf("GET[%d] differs between runs", i)
		}
	}
}

// ---- Swagger 2.0 ---------------------------------------------------------

func TestSwagger2_Petstore(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.swagger.json")

	if len(cfg.GET) != 2 || len(cfg.POST) != 1 {
		t.Errorf("group sizes: GET=%d POST=%d, want 2/1", len(cfg.GET), len(cfg.POST))
	}
	if cfg.GET[0].URL != "https://petstore.example.com/v1/pets" {
		t.Errorf("URL construction wrong: %q", cfg.GET[0].URL)
	}
}

func TestSwagger2_BodyParameterAndXExample(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.swagger.json")
	post := cfg.POST[0]
	if !strings.Contains(post.Body, `"name":"Rex"`) {
		t.Errorf("body = %q, want JSON containing name=Rex", post.Body)
	}
	if got := post.Headers["Content-Type"]; got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (from consumes)", got)
	}
}

func TestSwagger2_GlobalSecurityEmitsAuthHeader(t *testing.T) {
	cfg := loadFixture(t, "testdata/petstore.swagger.json")
	for _, ep := range cfg.GET {
		if got := ep.Headers["Authorization"]; got != AuthPlaceholder {
			t.Errorf("%s.Authorization = %q, want %q", ep.Name, got, AuthPlaceholder)
		}
	}
}

func TestSwagger2_EmptyHostIsAnError(t *testing.T) {
	spec := `{"swagger":"2.0","paths":{"/x":{"get":{}}}}`
	_, err := OpenAPI(strings.NewReader(spec), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty host") {
		t.Errorf("want empty-host error, got %v", err)
	}
}

// ---- Format detection ----------------------------------------------------

func TestDetectDialect_UnknownVersion(t *testing.T) {
	_, err := OpenAPI(strings.NewReader(`{"openapi":"4.0.0"}`), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported OpenAPI version") {
		t.Errorf("want unsupported-version error, got %v", err)
	}

	_, err = OpenAPI(strings.NewReader(`{"swagger":"1.2"}`), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported Swagger version") {
		t.Errorf("want unsupported-swagger error, got %v", err)
	}
}

func TestDetectDialect_NotASpec(t *testing.T) {
	_, err := OpenAPI(strings.NewReader(`{"random":"json"}`), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "not a recognised") {
		t.Errorf("want not-a-spec error, got %v", err)
	}
}

func TestDetectDialect_NotJSON(t *testing.T) {
	_, err := OpenAPI(strings.NewReader("this is not JSON"), OpenAPIOptions{})
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("want invalid-JSON error, got %v", err)
	}
}

// ---- Loaded Config validates against the schema --------------------------

// The whole point of the importer is to produce something the runtime
// loader can consume. End-to-end check: take the importer's output,
// run it through config.Validate, expect no error.
func TestImportedConfigPassesValidation(t *testing.T) {
	cases := []string{
		"testdata/petstore.openapi.json",
		"testdata/petstore.openapi.yaml",
		"testdata/petstore.swagger.json",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			cfg := loadFixture(t, path)
			if err := cfg.Validate(); err != nil {
				t.Errorf("imported config fails Validate: %v", err)
			}
		})
	}
}

// ---- YAML support --------------------------------------------------------

func TestOpenAPI3_YAMLProducesSameShapeAsJSON(t *testing.T) {
	jsonCfg := loadFixture(t, "testdata/petstore.openapi.json")
	yamlCfg := loadFixture(t, "testdata/petstore.openapi.yaml")

	// The YAML fixture has /pets, /pets/{petId} (no /admin/stats), so
	// counts differ — but it should still parse cleanly into the same
	// schema-valid Config shape.
	if yamlCfg.Schema != jsonCfg.Schema || yamlCfg.Version != jsonCfg.Version {
		t.Errorf("YAML produced different schema/version than JSON: %+v vs %+v",
			yamlCfg, jsonCfg)
	}
	if len(yamlCfg.GET) != 2 || len(yamlCfg.POST) != 1 {
		t.Errorf("YAML group sizes: GET=%d POST=%d, want 2/1", len(yamlCfg.GET), len(yamlCfg.POST))
	}
	post := yamlCfg.POST[0]
	if post.Name != "createPet" || !strings.Contains(post.Body, "Whiskers") {
		t.Errorf("YAML POST mapping wrong: %+v", post)
	}
}

// ---- Filters -------------------------------------------------------------

func TestOpenAPI3_TagFilterIncludesOnlyMatching(t *testing.T) {
	f, _ := os.Open("testdata/petstore.openapi.json")
	defer f.Close()
	cfg, err := OpenAPI(f, OpenAPIOptions{TagFilter: "admin"})
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	// Only adminStats has the "admin" tag.
	if len(cfg.GET) != 1 || cfg.GET[0].Name != "adminStats" {
		t.Errorf("tag=admin should yield only adminStats, got %v", endpointNames(cfg.GET))
	}
	if len(cfg.POST) != 0 {
		t.Errorf("tag=admin should produce no POST endpoints, got %d", len(cfg.POST))
	}
}

func TestOpenAPI3_PathPrefixIncludesOnlyMatching(t *testing.T) {
	f, _ := os.Open("testdata/petstore.openapi.json")
	defer f.Close()
	cfg, err := OpenAPI(f, OpenAPIOptions{PathPrefix: "/admin"})
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	if len(cfg.GET) != 1 || cfg.GET[0].Name != "adminStats" {
		t.Errorf("path-prefix=/admin should yield only adminStats, got %v", endpointNames(cfg.GET))
	}
}

func TestOpenAPI3_TagAndPathPrefixCombined(t *testing.T) {
	f, _ := os.Open("testdata/petstore.openapi.json")
	defer f.Close()
	// tag=pets matches 4 ops, path=/pets prefix matches 3, combined = 3.
	cfg, err := OpenAPI(f, OpenAPIOptions{TagFilter: "pets", PathPrefix: "/pets"})
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	if len(cfg.GET) != 2 || len(cfg.POST) != 1 {
		t.Errorf("combined filter: GET=%d POST=%d, want 2/1; got names=%v",
			len(cfg.GET), len(cfg.POST), endpointNames(cfg.GET))
	}
}

func TestOpenAPI3_FilterMatchingNothingProducesEmptyConfig(t *testing.T) {
	f, _ := os.Open("testdata/petstore.openapi.json")
	defer f.Close()
	cfg, err := OpenAPI(f, OpenAPIOptions{TagFilter: "no-such-tag"})
	if err != nil {
		t.Fatalf("OpenAPI: %v", err)
	}
	// Importer doesn't enforce "at least one endpoint" — that's the
	// runtime loader's job. An empty filter result is a valid Config
	// shape, but config.Validate() will reject it as "no endpoints
	// defined" if a user tries to run it. That's the right
	// separation of concerns: the importer reports what filtered,
	// the loader reports what's runnable.
	for _, g := range cfg.Groups() {
		if len(g.Endpoints) != 0 {
			t.Errorf("expected zero endpoints, got %d under %s", len(g.Endpoints), g.Method)
		}
	}
}

// endpointNames flattens a Endpoints slice down to names so test
// output stays readable.
func endpointNames(eps []config.Endpoint) []string {
	out := make([]string, len(eps))
	for i, ep := range eps {
		out[i] = ep.Name
	}
	return out
}

func TestLooksLikeJSON(t *testing.T) {
	cases := []struct {
		in   string
		json bool
	}{
		{`{"a":1}`, true},
		{"  \n\t{ }", true},
		{`["a"]`, true},
		{"openapi: 3.0.0\n", false},
		{"---\nfoo: bar\n", false},
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikeJSON([]byte(c.in)); got != c.json {
			t.Errorf("looksLikeJSON(%q) = %v, want %v", c.in, got, c.json)
		}
	}
}
