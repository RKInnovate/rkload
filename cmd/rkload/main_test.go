package main

import (
	"flag"
	"strings"
	"testing"
)

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

// ---- parseVarFlags -------------------------------------------------------
//
// parseVarFlags is the user-facing entry point for the repeatable
// `--var key=value` flag on `rkload import postman`. A confusing
// failure here would be hard to diagnose from the wrong end of a
// shell prompt, so the cases below pin every documented behaviour
// (and a couple of quietly-supported ones).

func TestParseVarFlags_Empty(t *testing.T) {
	got, err := parseVarFlags(nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil map for empty input", got)
	}
}

func TestParseVarFlags_SingleAndMultiple(t *testing.T) {
	got, err := parseVarFlags([]string{"baseUrl=https://x", "token=abc"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["baseUrl"] != "https://x" || got["token"] != "abc" {
		t.Errorf("got = %v, want both keys mapped", got)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestParseVarFlags_MalformedHasNoEquals(t *testing.T) {
	_, err := parseVarFlags([]string{"justakey"})
	if err == nil {
		t.Fatal("expected error for missing =, got nil")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Errorf("error should suggest key=value form, got: %v", err)
	}
}

func TestParseVarFlags_EmptyKeyRejected(t *testing.T) {
	// "=value" places the equals at index 0, which our `eq <= 0`
	// guard catches. Empty keys would silently overwrite each other
	// in the resulting map — better to error.
	_, err := parseVarFlags([]string{"=oops"})
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

// TestParseVarFlags_ValueMayContainEquals: "k=a=b" splits on the FIRST
// equals — token strings (JWTs especially) frequently contain `=`
// padding, so swallowing them silently would be a UX trap.
func TestParseVarFlags_ValueMayContainEquals(t *testing.T) {
	got, err := parseVarFlags([]string{"jwt=eyJhbGc=.payload=.sig"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["jwt"] != "eyJhbGc=.payload=.sig" {
		t.Errorf("value = %q, want full string with embedded =", got["jwt"])
	}
}

func TestParseVarFlags_EmptyValueAccepted(t *testing.T) {
	got, err := parseVarFlags([]string{"emptyval="})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if v, ok := got["emptyval"]; !ok || v != "" {
		t.Errorf("got = %v, want emptyval mapped to empty string", got)
	}
}

// ---- repeatableFlag ------------------------------------------------------
//
// repeatableFlag is a custom flag.Value backing the `--var` flag.
// Tests exercise it both directly and through a flag.FlagSet to catch
// any regressions in how the stdlib calls Set/String on it.

func TestRepeatableFlag_AccumulatesAcrossCalls(t *testing.T) {
	var values []string
	rf := &repeatableFlag{values: &values}
	for _, v := range []string{"a=1", "b=2", "c=3"} {
		if err := rf.Set(v); err != nil {
			t.Fatalf("Set(%q) = %v", v, err)
		}
	}
	if len(values) != 3 {
		t.Fatalf("len = %d, want 3", len(values))
	}
	if rf.String() != "a=1,b=2,c=3" {
		t.Errorf("String() = %q, want %q", rf.String(), "a=1,b=2,c=3")
	}
}

func TestRepeatableFlag_ZeroValueStringIsEmpty(t *testing.T) {
	// flag.PrintDefaults calls String() on a zero-value flag.Value to
	// render the help text. Returning empty (not panicking on nil
	// values pointer) is the contract.
	var rf repeatableFlag
	if got := rf.String(); got != "" {
		t.Errorf("zero-value String() = %q, want empty", got)
	}
}

func TestRepeatableFlag_ViaFlagSet(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values := newRepeatableFlag(fs, "var", "")

	args := []string{"--var", "k1=v1", "--var", "k2=v2", "--var", "k3=v3"}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(*values) != 3 {
		t.Errorf("len = %d, want 3", len(*values))
	}
	want := []string{"k1=v1", "k2=v2", "k3=v3"}
	for i, v := range *values {
		if v != want[i] {
			t.Errorf("values[%d] = %q, want %q", i, v, want[i])
		}
	}
}

// TestRepeatableFlag_NoSetCalls verifies the slice stays empty when
// the flag is never used — important because parseVarFlags treats
// len==0 as "no overrides" and returns nil rather than an empty map.
func TestRepeatableFlag_NoSetCalls(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	values := newRepeatableFlag(fs, "var", "")
	if err := fs.Parse([]string{}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(*values) != 0 {
		t.Errorf("len = %d, want 0", len(*values))
	}
}
