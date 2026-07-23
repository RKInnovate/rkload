package config

import (
	"path/filepath"
	"testing"
)

// TestExampleConfigs_Load guards the shipped docs/examples against drift:
// each must load and validate, so a doc example a user copy-pastes can't
// rot into an invalid config. Covers the v1 endpoint example and the v2
// scenario example.
func TestExampleConfigs_Load(t *testing.T) {
	for _, path := range []string{
		"../../docs/examples/basic.config.json",
		"../../docs/examples/scenario.config.json",
	} {
		if _, err := Load(path); err != nil {
			t.Errorf("%s should load and validate, got: %v", path, err)
		}
	}
}

// TestHTTPBinExampleConfigs_Load guards every runnable httpbin worked
// example (single-request and scenario configs alike) — they must all
// load and validate so `rkload -config examples/httpbin/configs/...`
// keeps working out of the box.
func TestHTTPBinExampleConfigs_Load(t *testing.T) {
	matches, err := filepath.Glob("../../examples/httpbin/configs/*.rkload.json")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no httpbin example configs found")
	}
	for _, path := range matches {
		if _, err := Load(path); err != nil {
			t.Errorf("%s should load and validate, got: %v", path, err)
		}
	}
}
