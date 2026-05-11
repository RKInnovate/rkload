package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RKInnovate/rkload/internal/cache"
	"github.com/RKInnovate/rkload/internal/config"
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

// ---- runValidate ---------------------------------------------------------
//
// Every test that exercises runValidate redirects the cache to a
// fresh temp dir via t.Setenv so cache writes don't pollute the
// user's ~/.rkload/cache during `go test`.

const validConfigJSON = `{
  "$schema": "https://raw.githubusercontent.com/RKInnovate/rkload/main/schemas/v1/config.schema.json",
  "version": 1,
  "GET":  [{"url": "https://example.com/a"}, {"url": "https://example.com/b"}],
  "POST": [{"url": "https://example.com/p"}]
}`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rkload.config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestRunValidate_SuccessWritesCacheEntry(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cache.EnvDirOverride, cacheDir)
	path := writeTempConfig(t, validConfigJSON)

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, path, false); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	entries, _ := os.ReadDir(cacheDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".json") {
		t.Errorf("cache file name = %q, want .json suffix", entries[0].Name())
	}
}

func TestRunValidate_NoCacheFlagSuppressesWrite(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cache.EnvDirOverride, cacheDir)
	path := writeTempConfig(t, validConfigJSON)

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, path, true); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 0 {
		t.Errorf("-no-cache should not write entries, found %d", len(entries))
	}
	if !strings.Contains(out.String(), "no (-no-cache)") {
		t.Errorf("summary should report 'no (-no-cache)', got:\n%s", out.String())
	}
}

func TestRunValidate_SummaryContainsExpectedFields(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	path := writeTempConfig(t, validConfigJSON)

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, path, false); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	got := out.String()
	for _, want := range []string{
		"Validated:", "Status:    valid", "Hash:", "Size:",
		"Schema:", "Version:   1", "Endpoints: GET=2, POST=1 (total: 3)", "Cached:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q. Full output:\n%s", want, got)
		}
	}
}

func TestRunValidate_MissingFile(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	var out, errOut bytes.Buffer
	code := runValidate(&out, &errOut, "/no/such/path/rkload.json", false)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Errorf("expected error message on stderr")
	}
}

func TestRunValidate_InvalidConfigRejected(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	// Missing required "version" field.
	path := writeTempConfig(t, `{"GET":[{"url":"https://example.com/"}]}`)

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, path, false); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "version") {
		t.Errorf("expected version error on stderr, got: %s", errOut.String())
	}
}

func TestRunValidate_UnknownFieldRejected(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	path := writeTempConfig(t, `{"version":1,"TRACE":[{"url":"https://example.com/"}]}`)

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, path, false); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "TRACE") {
		t.Errorf("error should name the unknown field, got: %s", errOut.String())
	}
}

func TestRunValidate_RecordsAbsolutePath(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cache.EnvDirOverride, cacheDir)
	path := writeTempConfig(t, validConfigJSON)

	// Pass a relative-ish path (basename only, with chdir) so we can
	// verify the entry stores the absolute path.
	dir := filepath.Dir(path)
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := runValidate(&out, &errOut, "rkload.config.json", false); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	entries, _ := os.ReadDir(cacheDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(entries))
	}
	raw, _ := os.ReadFile(filepath.Join(cacheDir, entries[0].Name()))
	// macOS resolves /var → /private/var; both forms count as
	// absolute, so don't pin the exact prefix — check that the path
	// is absolute and ends with the expected filename.
	var got struct {
		ConfigPath string `json:"config_path"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("entry was not valid JSON: %v", err)
	}
	if !filepath.IsAbs(got.ConfigPath) {
		t.Errorf("config_path %q is not absolute", got.ConfigPath)
	}
	if filepath.Base(got.ConfigPath) != "rkload.config.json" {
		t.Errorf("config_path basename = %q, want rkload.config.json", filepath.Base(got.ConfigPath))
	}
}

// ---- endpointCounts ------------------------------------------------------

func TestEndpointCounts_OmitsEmptyMethods(t *testing.T) {
	cfg := &config.Config{
		Version: 1,
		GET:     []config.Endpoint{{URL: "https://x/a"}, {URL: "https://x/b"}},
		POST:    []config.Endpoint{{URL: "https://x/p"}},
	}
	got := endpointCounts(cfg)
	if got["GET"] != 2 || got["POST"] != 1 || got["total"] != 3 {
		t.Errorf("counts = %v, want GET=2 POST=1 total=3", got)
	}
	if _, ok := got["PUT"]; ok {
		t.Errorf("PUT should be absent when no endpoints declared, got: %v", got)
	}
}

func TestFormatCounts_StableOrder(t *testing.T) {
	// PUT before DELETE before HEAD — alphabetical order would break
	// this; methodOrder preserves the canonical sequence.
	counts := map[string]int{"HEAD": 1, "DELETE": 1, "PUT": 1, "total": 3}
	got := formatCounts(counts)
	want := "PUT=1, DELETE=1, HEAD=1"
	if got != want {
		t.Errorf("formatCounts = %q, want %q", got, want)
	}
}

func TestFormatCounts_NoneFallback(t *testing.T) {
	got := formatCounts(map[string]int{"total": 0})
	if got != "none" {
		t.Errorf("formatCounts(empty) = %q, want %q", got, "none")
	}
}
