package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalHash_StableAcrossReformat(t *testing.T) {
	a := []byte(`{"version":1,"GET":[{"url":"https://example.com","c":10}]}`)
	b := []byte(`{
  "GET": [
    {
      "c": 10,
      "url": "https://example.com"
    }
  ],
  "version": 1
}`)
	ha, err := CanonicalHash(a)
	if err != nil {
		t.Fatalf("hash(a): %v", err)
	}
	hb, err := CanonicalHash(b)
	if err != nil {
		t.Fatalf("hash(b): %v", err)
	}
	if ha != hb {
		t.Errorf("expected reformat-invariant hash, got %s vs %s", ha, hb)
	}
}

func TestCanonicalHash_DifferentValuesDiffer(t *testing.T) {
	a := []byte(`{"version":1,"GET":[{"url":"https://example.com","c":10}]}`)
	b := []byte(`{"version":1,"GET":[{"url":"https://example.com","c":20}]}`)
	ha, _ := CanonicalHash(a)
	hb, _ := CanonicalHash(b)
	if ha == hb {
		t.Errorf("expected differing hashes for semantically different configs, both %s", ha)
	}
}

func TestCanonicalHash_RejectsInvalidJSON(t *testing.T) {
	_, err := CanonicalHash([]byte(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parsing config for hashing") {
		t.Errorf("expected wrapped parse error, got: %v", err)
	}
}

func TestCanonicalHash_DeterministicOnRepeatedCalls(t *testing.T) {
	data := []byte(`{"version":1,"GET":[{"url":"https://example.com"}]}`)
	first, _ := CanonicalHash(data)
	for i := 0; i < 5; i++ {
		again, err := CanonicalHash(data)
		if err != nil {
			t.Fatalf("hash repeat %d: %v", i, err)
		}
		if again != first {
			t.Fatalf("non-deterministic hash on repeat %d: %s vs %s", i, again, first)
		}
	}
}

func TestDir_HonorsEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != dir {
		t.Errorf("Dir() = %q, want %q", got, dir)
	}
}

func TestDir_FallsBackToHomeWhenEnvUnset(t *testing.T) {
	t.Setenv(EnvDirOverride, "")
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join(".rkload", "cache")) {
		t.Errorf("Dir() = %q, want suffix .rkload/cache", got)
	}
}

func TestStoreAndLookup_RoundTrip(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	entry := &Entry{
		Hash:           "abc123",
		ValidatedAt:    time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
		RkloadVersion:  "0.3.3",
		ConfigPath:     "/tmp/foo.json",
		FileSizeBytes:  256,
		SchemaURL:      "https://example.com/schemas/v1/config.schema.json",
		SchemaVersion:  1,
		EndpointCounts: map[string]int{"GET": 2, "POST": 1, "total": 3},
		Status:         StatusValid,
	}
	if err := Store(entry); err != nil {
		t.Fatalf("Store: %v", err)
	}
	got, err := Lookup("abc123")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got == nil {
		t.Fatal("Lookup returned nil for stored entry")
	}
	if got.Hash != entry.Hash || got.RkloadVersion != entry.RkloadVersion {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, entry)
	}
	if got.EndpointCounts["total"] != 3 {
		t.Errorf("EndpointCounts.total = %d, want 3", got.EndpointCounts["total"])
	}
}

func TestLookup_MissReturnsNilNil(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	got, err := Lookup("does-not-exist")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil on miss, got %+v", got)
	}
}

func TestLookup_CorruptFileTreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	if err := os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Lookup("corrupt")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for corrupt file, got %+v", got)
	}
}

func TestLookup_HashMismatchTreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDirOverride, dir)
	entry := Entry{Hash: "different-hash", Status: StatusValid}
	data, _ := json.Marshal(entry)
	if err := os.WriteFile(filepath.Join(dir, "expected-hash.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Lookup("expected-hash")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil when stored hash field disagrees with lookup key, got %+v", got)
	}
}

func TestStore_AtomicallyReplacesExisting(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	first := &Entry{Hash: "h", Status: StatusValid, RkloadVersion: "0.3.2"}
	if err := Store(first); err != nil {
		t.Fatalf("Store first: %v", err)
	}
	second := &Entry{Hash: "h", Status: StatusValid, RkloadVersion: "0.3.3"}
	if err := Store(second); err != nil {
		t.Fatalf("Store second: %v", err)
	}
	got, err := Lookup("h")
	if err != nil || got == nil {
		t.Fatalf("Lookup after second Store: got=%v err=%v", got, err)
	}
	if got.RkloadVersion != "0.3.3" {
		t.Errorf("expected second write to win, got RkloadVersion=%q", got.RkloadVersion)
	}
}

func TestStore_RejectsEmptyHash(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	err := Store(&Entry{Hash: "", Status: StatusValid})
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestStore_RejectsNilEntry(t *testing.T) {
	t.Setenv(EnvDirOverride, t.TempDir())
	err := Store(nil)
	if err == nil {
		t.Fatal("expected error for nil entry")
	}
}
