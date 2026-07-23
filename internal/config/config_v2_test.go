package config

import (
	"strings"
	"testing"
)

// ---- Multi-version acceptance (schema v2) --------------------------------

func TestValidate_UnsupportedVersionListsSupported(t *testing.T) {
	c := &Config{Version: 99, GET: []Endpoint{{URL: "https://example.com/"}}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "supports 1, 2") {
		t.Errorf("want error listing supported versions, got %v", err)
	}
}

func TestValidate_AcceptsV2(t *testing.T) {
	c := &Config{
		Version:   2,
		Scenarios: []Scenario{{Name: "s", Steps: []Step{{URL: "https://example.com/"}}}},
	}
	if err := c.Validate(); err != nil {
		t.Errorf("v2 config with a scenario should validate, got %v", err)
	}
}

func TestValidate_ScenariosRequireV2(t *testing.T) {
	c := &Config{
		Version:   1,
		Scenarios: []Scenario{{Name: "s", Steps: []Step{{URL: "https://example.com/"}}}},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "requires schema version 2") {
		t.Errorf("want scenarios-require-v2 error, got %v", err)
	}
}

func TestValidate_V2EmptyMessage(t *testing.T) {
	c := &Config{Version: 2}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "no endpoints or scenarios defined") {
		t.Errorf("want v2 empty error, got %v", err)
	}
}

// ---- Parse strictness on the v2 surface ----------------------------------

func TestParse_RejectsUnknownScenarioField(t *testing.T) {
	raw := []byte(`{"version":2,"scenarios":[{"name":"s","banana":true,"steps":[]}]}`)
	_, err := Parse(raw)
	if err == nil {
		t.Fatal("expected unknown-field error")
	}
	if !strings.Contains(err.Error(), "banana") {
		t.Errorf("error should name the offending field, got: %v", err)
	}
}
