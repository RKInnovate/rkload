package config

import (
	"strings"
	"testing"
)

// ---- Load (v2 round-trip from testdata) ----------------------------------

func TestLoad_ValidV2(t *testing.T) {
	c, err := Load("testdata/valid_v2.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Version != 2 {
		t.Errorf("Version = %d, want 2", c.Version)
	}
	// v2 is a superset: endpoints and scenarios coexist.
	if len(c.GET) != 1 {
		t.Errorf("GET = %d, want 1", len(c.GET))
	}
	if len(c.Scenarios) != 1 {
		t.Fatalf("Scenarios = %d, want 1", len(c.Scenarios))
	}

	s := c.Scenarios[0]
	if s.Name != "login-call-logout" || s.VUs != 5 || s.Iterations != 50 || s.Timeout != "10s" {
		t.Errorf("scenario fields wrong: %+v", s)
	}
	if s.Auth == nil || s.Auth.Type != "bearer" || s.Auth.Token != "${API_TOKEN}" {
		t.Errorf("scenario auth lost: %+v", s.Auth)
	}
	if len(s.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(s.Steps))
	}

	login := s.Steps[0]
	if login.Method != "POST" || login.Name != "login" {
		t.Errorf("step 0 method/name wrong: %+v", login)
	}
	if len(login.Extract) != 2 || login.Extract[0].Var != "token" || login.Extract[0].From != "json" || login.Extract[0].Path != "data.accessToken" {
		t.Errorf("extract rules lost: %+v", login.Extract)
	}
	if login.Extract[1].From != "header" || login.Extract[1].Name != "X-Request-Id" {
		t.Errorf("header extract lost: %+v", login.Extract[1])
	}
	if len(login.Assert) != 3 || login.Assert[0].Type != "status" || login.Assert[0].Equals != 200 {
		t.Errorf("assert rules lost: %+v", login.Assert)
	}
	// Injection placeholders are preserved verbatim (resolved at run time).
	if got := s.Steps[1].Headers["Authorization"]; got != "Bearer ${token}" {
		t.Errorf("injection placeholder lost: %q", got)
	}
}

// ---- Scenario / step structural validation -------------------------------

func TestValidate_ScenarioNeedsStep(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{Name: "empty"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "at least one step") {
		t.Errorf("want needs-step error, got %v", err)
	}
}

func TestValidate_StepURLRequired(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{Steps: []Step{{Method: "GET"}}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `"url" is required`) {
		t.Errorf("want step-url-required error, got %v", err)
	}
	if !strings.Contains(err.Error(), "scenarios[0].steps[0]") {
		t.Errorf("error should locate the step, got %v", err)
	}
}

func TestValidate_StepURLScheme(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{Steps: []Step{{URL: "ftp://example.com/"}}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Errorf("want step-url-scheme error, got %v", err)
	}
}

func TestValidate_StepUnknownMethod(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{Steps: []Step{{URL: "https://example.com/", Method: "FETCH"}}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `unknown method "FETCH"`) {
		t.Errorf("want unknown-method error, got %v", err)
	}
}

func TestValidate_ScenarioVUsOutOfRange(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{VUs: 100000, Steps: []Step{{URL: "https://example.com/"}}}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("want vus-range error, got %v", err)
	}
}

// ---- Defaults on scenarios -----------------------------------------------

func TestApplyDefaults_Scenario(t *testing.T) {
	c := &Config{
		Version:   2,
		Scenarios: []Scenario{{Steps: []Step{{URL: "https://example.com/"}}}},
	}
	c.ApplyDefaults()
	c.ApplyDefaults() // idempotent
	s := c.Scenarios[0]
	if s.VUs != DefaultConcurrency || s.Iterations != DefaultRequests || s.Timeout != DefaultTimeout {
		t.Errorf("scenario defaults not applied: %+v", s)
	}
	if s.Steps[0].Method != "GET" {
		t.Errorf("step method default = %q, want GET", s.Steps[0].Method)
	}
}
