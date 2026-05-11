package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RKInnovate/rkload/internal/cache"
	"github.com/RKInnovate/rkload/internal/config"
	"github.com/RKInnovate/rkload/internal/updater"
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

// ---- runUpdate -----------------------------------------------------------
//
// runUpdate is exercised end-to-end against a fake GitHub set up via
// httptest. The fake serves a tarball whose checksum matches the
// checksums.txt it advertises, so the verification step is real and
// any regression there would break these tests.

// fakeGitHubForUpdate spins up an httptest server that handles both
// /repos/.../releases/latest (API) and the asset downloads. The
// archive contains a single "rkload" entry with newContent so the
// caller can verify which version landed after replacement.
func fakeGitHubForUpdate(t *testing.T, tag, goos, goarch string, newContent []byte) string {
	t.Helper()
	archiveName := updater.ArchiveName(tag, goos, goarch)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "rkload", Size: int64(len(newContent)), Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(newContent); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			fmt.Fprintf(w, `{"tag_name": %q, "assets": []}`, tag)
		case strings.HasSuffix(r.URL.Path, archiveName):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	oldAPI, oldRedirect := updater.APIBase, updater.ReleaseRedirectBase
	updater.APIBase = srv.URL
	updater.ReleaseRedirectBase = srv.URL
	t.Cleanup(func() {
		updater.APIBase = oldAPI
		updater.ReleaseRedirectBase = oldRedirect
		srv.Close()
	})
	return srv.URL
}

// writeFakeBinary creates a temporary file we can pretend is the
// running rkload binary for ReplaceSelf to swap out.
func writeFakeBinary(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rkload")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunUpdate_CheckPrintsAvailable(t *testing.T) {
	// version (build-time var) is "dev" in tests → always older than
	// the v0.99.0 we advertise → check should report availability.
	fakeGitHubForUpdate(t, "v0.99.0", "linux", "amd64", []byte("NEW BYTES"))
	exe := writeFakeBinary(t, []byte("OLD BYTES"))

	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64",
		true /* check */, "" /* pinned */, false /* force */)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "v0.99.0 available") {
		t.Errorf("output should mention v0.99.0 available; got: %s", out.String())
	}
	// --check must not modify the binary.
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD BYTES" {
		t.Errorf("--check modified the binary; content = %q", got)
	}
}

func TestRunUpdate_DownloadsAndReplaces(t *testing.T) {
	fakeGitHubForUpdate(t, "v0.99.0", "linux", "amd64", []byte("NEW BYTES"))
	exe := writeFakeBinary(t, []byte("OLD BYTES"))

	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64", false, "", false)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "NEW BYTES" {
		t.Errorf("binary not replaced; content = %q", got)
	}
	if !strings.Contains(out.String(), "Updated rkload to v0.99.0") {
		t.Errorf("expected success summary; got: %s", out.String())
	}
}

func TestRunUpdate_AlreadyUpToDate(t *testing.T) {
	// Save and override the package-level version so Newer() reports
	// false. Restore in cleanup so other tests aren't affected.
	saved := version
	version = "v0.99.0"
	t.Cleanup(func() { version = saved })

	fakeGitHubForUpdate(t, "v0.99.0", "linux", "amd64", []byte("NEW BYTES"))
	exe := writeFakeBinary(t, []byte("OLD BYTES"))

	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64", false, "", false)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "already up to date") {
		t.Errorf("expected 'already up to date'; got: %s", out.String())
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD BYTES" {
		t.Errorf("binary modified despite up-to-date; content = %q", got)
	}
}

func TestRunUpdate_ForceInstallsEvenWhenCurrent(t *testing.T) {
	saved := version
	version = "v0.99.0"
	t.Cleanup(func() { version = saved })

	fakeGitHubForUpdate(t, "v0.99.0", "linux", "amd64", []byte("FORCED"))
	exe := writeFakeBinary(t, []byte("OLD BYTES"))

	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64", false, "", true /* force */)
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errOut.String())
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "FORCED" {
		t.Errorf("--force should reinstall; got: %q", got)
	}
}

func TestRunUpdate_PinnedVersionSkipsNewerCheck(t *testing.T) {
	saved := version
	version = "v0.99.0" // way newer than the pinned target below
	t.Cleanup(func() { version = saved })

	// Latest API will report v0.99.0 but we're pinning to v0.0.1 so
	// the API call shouldn't matter — verify by NOT setting up an
	// API-style handler at all (only the asset paths).
	archiveName := updater.ArchiveName("v0.0.1", "linux", "amd64")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("PINNED VERSION BYTES")
	_ = tw.WriteHeader(&tar.Header{Name: "rkload", Size: int64(len(body)), Mode: 0o755})
	_, _ = tw.Write(body)
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			_, _ = w.Write(buf.Bytes())
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	oldRedirect := updater.ReleaseRedirectBase
	updater.ReleaseRedirectBase = srv.URL
	t.Cleanup(func() {
		updater.ReleaseRedirectBase = oldRedirect
		srv.Close()
	})

	exe := writeFakeBinary(t, []byte("OLD BYTES"))
	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64", false, "v0.0.1", false)
	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, errOut.String())
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "PINNED VERSION BYTES" {
		t.Errorf("pinned downgrade did not install; got: %q", got)
	}
}

func TestRunUpdate_NetworkError(t *testing.T) {
	// Point at a server that always 500s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	oldAPI, oldRedirect := updater.APIBase, updater.ReleaseRedirectBase
	updater.APIBase = srv.URL
	updater.ReleaseRedirectBase = srv.URL
	t.Cleanup(func() {
		updater.APIBase = oldAPI
		updater.ReleaseRedirectBase = oldRedirect
	})

	exe := writeFakeBinary(t, []byte("OLD"))
	var out, errOut bytes.Buffer
	code := runUpdate(&out, &errOut, exe, "linux", "amd64", false, "", false)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if errOut.Len() == 0 {
		t.Errorf("expected error on stderr")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD" {
		t.Errorf("network failure should not modify binary; got: %q", got)
	}
}

// ---- runInit -------------------------------------------------------------
//
// runInit emits a starter config; the tests below pin the three
// invocation modes (stdout, file, file-exists) plus the contract
// that whatever runInit writes must validate as a real rkload
// config — otherwise the template has rotted since the schema or
// validator changed.

func TestRunInit_DefaultsToStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, "", false); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"$schema"`) {
		t.Errorf("stdout should contain $schema field, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"version": 1`) {
		t.Errorf("stdout should contain version: 1, got:\n%s", out.String())
	}
}

func TestRunInit_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rkload.config.json")

	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, path, false); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), `"login"`) {
		t.Errorf("file should contain example POST endpoint, got:\n%s", data)
	}
	if !strings.Contains(out.String(), "Wrote starter config") {
		t.Errorf("expected 'Wrote starter config' on stdout, got: %s", out.String())
	}
}

func TestRunInit_RefusesOverwriteByDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rkload.config.json")
	if err := os.WriteFile(path, []byte("existing content"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, path, false); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("stderr should mention 'already exists', got: %s", errOut.String())
	}
	// File contents preserved.
	if data, _ := os.ReadFile(path); string(data) != "existing content" {
		t.Errorf("existing file overwritten despite no --force; got: %s", data)
	}
}

func TestRunInit_ForceOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rkload.config.json")
	if err := os.WriteFile(path, []byte("existing content"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, path, true); code != 0 {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut.String())
	}
	data, _ := os.ReadFile(path)
	if string(data) == "existing content" {
		t.Error("--force should overwrite existing file")
	}
	if !strings.Contains(string(data), `"$schema"`) {
		t.Errorf("expected starter config after --force, got:\n%s", data)
	}
}

// TestRunInit_OutputValidatesAgainstSchema is the regression guard:
// if someone edits the template and breaks the schema (renaming
// "url" to "URL", omitting "version", etc.), this test fails before
// the broken template lands.
func TestRunInit_OutputValidatesAgainstSchema(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, "", false); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	cfg, err := config.Parse(out.Bytes())
	if err != nil {
		t.Fatalf("starter config does not parse: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("starter config does not validate: %v", err)
	}
}

func TestRunInit_RejectsParentDirMissing(t *testing.T) {
	// Pointing init at a path whose parent directory doesn't exist
	// should fail clearly, not silently create one or panic.
	path := filepath.Join(t.TempDir(), "does-not-exist", "rkload.config.json")

	var out, errOut bytes.Buffer
	if code := runInit(&out, &errOut, path, false); code != 1 {
		t.Errorf("exit code = %d, want 1 for missing parent dir", code)
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

// ---- loadAndValidateForRun ----------------------------------------------
//
// loadAndValidateForRun is the cache-aware loader used by the
// `-config` run flow. The tests below pin the cache hit / miss /
// version-mismatch behaviours that change whether Validate runs.

func TestLoadAndValidateForRun_CacheMissThenHit(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv(cache.EnvDirOverride, cacheDir)
	path := writeTempConfig(t, validConfigJSON)

	_, status1, err := loadAndValidateForRun(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if !strings.Contains(status1, "re-checked") {
		t.Errorf("first call status = %q, want it to mention re-checked", status1)
	}
	if entries, _ := os.ReadDir(cacheDir); len(entries) != 1 {
		t.Fatalf("expected 1 cache entry after first call, got %d", len(entries))
	}

	_, status2, err := loadAndValidateForRun(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !strings.Contains(status2, "cached") {
		t.Errorf("second call status = %q, want it to mention cached", status2)
	}
}

func TestLoadAndValidateForRun_VersionMismatchReValidates(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	path := writeTempConfig(t, validConfigJSON)

	// Prime the cache.
	if _, _, err := loadAndValidateForRun(path); err != nil {
		t.Fatalf("prime: %v", err)
	}

	// Mutate the saved entry to claim it was written by an older
	// rkload version. The next load should treat it as a miss.
	data, _ := os.ReadFile(path)
	hash, _ := cache.CanonicalHash(data)
	entry, _ := cache.Lookup(hash)
	if entry == nil {
		t.Fatal("entry vanished after prime call")
	}
	entry.RkloadVersion = "0.0.1-ancient"
	if err := cache.Store(entry); err != nil {
		t.Fatalf("re-store: %v", err)
	}

	_, status, err := loadAndValidateForRun(path)
	if err != nil {
		t.Fatalf("after mutation: %v", err)
	}
	if strings.Contains(status, "cached ") {
		t.Errorf("expected re-validation after version mismatch, got status: %q", status)
	}
	if !strings.Contains(status, "re-checked") {
		t.Errorf("expected status to mention re-checked, got: %q", status)
	}
}

func TestLoadAndValidateForRun_InvalidConfigReturnsError(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	path := writeTempConfig(t, `{"GET":[{"url":"https://example.com/"}]}`) // no version

	_, _, err := loadAndValidateForRun(path)
	if err == nil {
		t.Fatal("expected validation error for missing version")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version, got: %v", err)
	}
}

func TestLoadAndValidateForRun_MissingFile(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	_, _, err := loadAndValidateForRun("/nope/nada/nothing.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "opening") {
		t.Errorf("expected 'opening' in error, got: %v", err)
	}
}

func TestLoadAndValidateForRun_CacheWriteFailureNonFatal(t *testing.T) {
	// Point the cache at a path that cannot become a directory
	// (a regular file at the parent slot makes MkdirAll fail with
	// ENOTDIR), then verify validation still succeeds.
	parentDir := t.TempDir()
	blocker := filepath.Join(parentDir, "blocker")
	if err := os.WriteFile(blocker, []byte("regular file"), 0o644); err != nil {
		t.Fatalf("blocker setup: %v", err)
	}
	t.Setenv(cache.EnvDirOverride, filepath.Join(blocker, "cache"))
	path := writeTempConfig(t, validConfigJSON)

	cfg, status, err := loadAndValidateForRun(path)
	if err != nil {
		t.Fatalf("validation should not fail when cache write fails: %v", err)
	}
	if cfg == nil || cfg.Version != 1 {
		t.Errorf("expected usable config; got %+v", cfg)
	}
	if !strings.Contains(status, "cache write failed") {
		t.Errorf("status should mention cache write failure, got: %q", status)
	}
}

func TestLoadAndValidateForRun_AppliesDefaultsOnHit(t *testing.T) {
	t.Setenv(cache.EnvDirOverride, t.TempDir())
	// Endpoint omits c/requests/timeout, so defaults must be applied
	// even on the cache-hit path (or the loader gets c=0).
	bareEndpointConfig := `{
  "version": 1,
  "GET": [{"url": "https://example.com/health"}]
}`
	path := writeTempConfig(t, bareEndpointConfig)

	if _, _, err := loadAndValidateForRun(path); err != nil {
		t.Fatalf("prime: %v", err)
	}
	cfg, status, err := loadAndValidateForRun(path)
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if !strings.Contains(status, "cached") {
		t.Fatalf("expected cache hit on second call, status was %q", status)
	}
	if cfg.GET[0].Concurrency != config.DefaultConcurrency {
		t.Errorf("Concurrency on cached config = %d, want default %d (defaults must run on hit)",
			cfg.GET[0].Concurrency, config.DefaultConcurrency)
	}
	if cfg.GET[0].Timeout != config.DefaultTimeout {
		t.Errorf("Timeout on cached config = %q, want default %q", cfg.GET[0].Timeout, config.DefaultTimeout)
	}
}
