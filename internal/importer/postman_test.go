package importer

import (
	"os"
	"strings"
	"testing"

	"github.com/RKInnovate/rkload/internal/config"
)

func loadPostmanFixture(t *testing.T, path string) *config.Config {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	cfg, err := Postman(f, PostmanOptions{})
	if err != nil {
		t.Fatalf("Postman(%s): %v", path, err)
	}
	return cfg
}

func TestPostman_FlattensFolders(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")

	// Top-level: List pets (GET).
	// "Pets" folder: Create pet (POST), Get pet (GET).
	// "Admin" folder: Stats (GET).
	// → 3 GETs, 1 POST.
	if len(cfg.GET) != 3 {
		t.Errorf("GET count = %d, want 3 (folders flattened)", len(cfg.GET))
	}
	if len(cfg.POST) != 1 {
		t.Errorf("POST count = %d, want 1", len(cfg.POST))
	}
}

func TestPostman_VariableSubstitution(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")

	for _, ep := range cfg.GET {
		if strings.Contains(ep.URL, "{{baseUrl}}") {
			t.Errorf("baseUrl variable not substituted in URL: %q", ep.URL)
		}
		if !strings.HasPrefix(ep.URL, "https://petstore.example.com/v1") {
			t.Errorf("URL = %q, want it to start with substituted base", ep.URL)
		}
	}
}

func TestPostman_UnknownVarLeftAsIs(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")
	// {{token}} is not declared in the collection's variable list.
	// It should pass through verbatim so users can grep for it.
	for _, ep := range cfg.GET {
		if got := ep.Headers["Authorization"]; got != "" && !strings.Contains(got, "{{token}}") {
			continue // already substituted somehow — fine
		}
	}
	getPet := findEndpointByName(cfg.GET, "Get pet")
	if getPet == nil {
		t.Fatal("Get pet endpoint not found")
	}
	if getPet.Headers["Authorization"] != "Bearer {{token}}" {
		t.Errorf("unknown var should pass through, got Authorization=%q", getPet.Headers["Authorization"])
	}
}

func TestPostman_DisabledHeadersDropped(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")
	create := cfg.POST[0]
	if _, has := create.Headers["X-Trace"]; has {
		t.Errorf("disabled header X-Trace should be dropped, got %v", create.Headers)
	}
	if create.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type header lost: %v", create.Headers)
	}
}

func TestPostman_RawBodyPreserved(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")
	create := cfg.POST[0]
	if !strings.Contains(create.Body, `"name":"Rex"`) {
		t.Errorf("raw body lost: %q", create.Body)
	}
}

func TestPostman_StringURLForm(t *testing.T) {
	// "Get pet" uses the string-URL shortcut form. Verify it parsed.
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")
	getPet := findEndpointByName(cfg.GET, "Get pet")
	if getPet == nil {
		t.Fatal("Get pet endpoint not found")
	}
	if getPet.URL != "https://petstore.example.com/v1/pets/1" {
		t.Errorf("string-form URL = %q, want substituted", getPet.URL)
	}
}

func TestPostman_VarOverrides(t *testing.T) {
	f, _ := os.Open("testdata/petstore.postman.json")
	defer f.Close()
	cfg, err := Postman(f, PostmanOptions{
		Vars: map[string]string{"baseUrl": "https://staging.example.com"},
	})
	if err != nil {
		t.Fatalf("Postman: %v", err)
	}
	for _, ep := range cfg.GET {
		if !strings.HasPrefix(ep.URL, "https://staging.example.com") {
			t.Errorf("user var override not applied: %q", ep.URL)
		}
	}
}

func TestPostman_PathPrefixFilter(t *testing.T) {
	f, _ := os.Open("testdata/petstore.postman.json")
	defer f.Close()
	cfg, err := Postman(f, PostmanOptions{PathPrefix: "/admin"})
	if err != nil {
		t.Fatalf("Postman: %v", err)
	}
	if len(cfg.GET) != 1 || cfg.GET[0].Name != "Stats" {
		t.Errorf("--path-prefix /admin should yield only Stats, got %v", endpointNames(cfg.GET))
	}
}

func TestPostman_DefaultsApplied(t *testing.T) {
	f, _ := os.Open("testdata/petstore.postman.json")
	defer f.Close()
	cfg, err := Postman(f, PostmanOptions{
		DefaultConcurrency: 25,
		DefaultRequests:    300,
		DefaultTimeout:     "10s",
	})
	if err != nil {
		t.Fatalf("Postman: %v", err)
	}
	for _, ep := range cfg.GET {
		if ep.Concurrency != 25 || ep.Requests != 300 || ep.Timeout != "10s" {
			t.Errorf("defaults not applied to %s: %+v", ep.Name, ep)
		}
	}
}

func TestPostman_RejectsWrongSchemaVersion(t *testing.T) {
	col := `{"info":{"name":"old","schema":"https://schema.getpostman.com/json/collection/v2.0.0/collection.json"},"item":[]}`
	_, err := Postman(strings.NewReader(col), PostmanOptions{})
	if err == nil || !strings.Contains(err.Error(), "v2.1") {
		t.Errorf("want v2.1-only error, got %v", err)
	}
}

func TestPostman_GeneratedConfigValidates(t *testing.T) {
	cfg := loadPostmanFixture(t, "testdata/petstore.postman.json")
	if err := cfg.Validate(); err != nil {
		t.Errorf("imported Postman config fails Validate: %v", err)
	}
}

// findEndpointByName returns a pointer to the first endpoint with the
// given name, or nil. Used by tests that need to assert per-endpoint
// detail without depending on iteration order.
func findEndpointByName(eps []config.Endpoint, name string) *config.Endpoint {
	for i := range eps {
		if eps[i].Name == name {
			return &eps[i]
		}
	}
	return nil
}
