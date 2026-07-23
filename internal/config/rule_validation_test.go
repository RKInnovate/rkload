package config

import (
	"strings"
	"testing"
)

// stepScenario wraps a single step in a v2 config for rule-level tests.
func stepScenario(st Step) *Config {
	return &Config{Version: 2, Scenarios: []Scenario{{Steps: []Step{st}}}}
}

// ---- Extract rule validation ---------------------------------------------

func TestValidate_ExtractVarRequired(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Extract: []ExtractRule{{From: "status"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `"var" is required`) {
		t.Errorf("want extract-var-required error, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "steps[0].extract[0]") {
		t.Errorf("error should locate the rule, got %v", err)
	}
}

func TestValidate_ExtractUnknownSource(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Extract: []ExtractRule{{Var: "x", From: "xpath"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown extract source") {
		t.Errorf("want unknown-source error, got %v", err)
	}
}

func TestValidate_ExtractJSONNeedsPath(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Extract: []ExtractRule{{Var: "x", From: "json"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("want json-needs-path error, got %v", err)
	}
}

func TestValidate_ExtractRegexMustCompile(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Extract: []ExtractRule{{Var: "x", From: "regex", Pattern: "([0-9"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("want invalid-regex error, got %v", err)
	}
}

// ---- Assert rule validation ----------------------------------------------

func TestValidate_AssertUnknownType(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Assert: []AssertRule{{Type: "matches"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown assert type") {
		t.Errorf("want unknown-assert-type error, got %v", err)
	}
}

func TestValidate_AssertStatusNeedsEquals(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Assert: []AssertRule{{Type: "status"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "equals") {
		t.Errorf("want status-needs-equals error, got %v", err)
	}
}

func TestValidate_AssertJSONEqualsNeedsPath(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Assert: []AssertRule{{Type: "json-equals", Value: "x"}}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("want json-equals-needs-path error, got %v", err)
	}
}

// ---- Auth validation (scenario- and step-level) --------------------------

func TestValidate_AuthUnknownType(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{
		Auth:  &Auth{Type: "hawk"},
		Steps: []Step{{URL: "https://example.com/"}},
	}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown auth type") {
		t.Errorf("want unknown-auth-type error, got %v", err)
	}
}

func TestValidate_AuthBearerNeedsToken(t *testing.T) {
	c := &Config{Version: 2, Scenarios: []Scenario{{
		Auth:  &Auth{Type: "bearer"},
		Steps: []Step{{URL: "https://example.com/"}},
	}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("want bearer-needs-token error, got %v", err)
	}
}

func TestValidate_StepAuthOverrideValidated(t *testing.T) {
	c := stepScenario(Step{URL: "https://example.com/", Auth: &Auth{Type: "basic"}})
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "username") {
		t.Errorf("want step-auth basic-needs-username error, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "steps[0].auth") {
		t.Errorf("error should locate the step auth, got %v", err)
	}
}
