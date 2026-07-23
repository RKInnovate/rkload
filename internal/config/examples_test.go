package config

import "testing"

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
