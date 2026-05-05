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
