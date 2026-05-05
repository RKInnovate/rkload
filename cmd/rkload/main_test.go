package main

import "testing"

// TestVersionDefaults sanity-checks that the build-time variables have
// non-empty defaults so a `-version` invocation in dev mode does not panic
// or print blanks.
func TestVersionDefaults(t *testing.T) {
	cases := map[string]string{
		"version": version,
		"commit":  commit,
		"date":    date,
	}
	for name, value := range cases {
		if value == "" {
			t.Errorf("build variable %q is empty; expected a default", name)
		}
	}
}
