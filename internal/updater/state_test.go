package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStateDir_HonorsEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvStateDirOverride, dir)
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if got != dir {
		t.Errorf("StateDir = %q, want %q", got, dir)
	}
}

func TestStateDir_FallsBackToHomeRkload(t *testing.T) {
	t.Setenv(EnvStateDirOverride, "")
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if !strings.HasSuffix(got, ".rkload") {
		t.Errorf("StateDir = %q, want suffix .rkload", got)
	}
}

func TestSaveAndLoadState_RoundTrip(t *testing.T) {
	t.Setenv(EnvStateDirOverride, t.TempDir())
	in := &State{
		LastCheckedAt:     time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC),
		LatestVersionSeen: "v0.3.4",
	}
	if err := SaveState(in); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	out, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if !out.LastCheckedAt.Equal(in.LastCheckedAt) {
		t.Errorf("LastCheckedAt = %v, want %v", out.LastCheckedAt, in.LastCheckedAt)
	}
	if out.LatestVersionSeen != in.LatestVersionSeen {
		t.Errorf("LatestVersionSeen = %q, want %q", out.LatestVersionSeen, in.LatestVersionSeen)
	}
}

func TestLoadState_NoFileReturnsEmpty(t *testing.T) {
	t.Setenv(EnvStateDirOverride, t.TempDir())
	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got == nil {
		t.Fatal("LoadState should return a non-nil State even when no file exists")
	}
	if !got.LastCheckedAt.IsZero() || got.LatestVersionSeen != "" {
		t.Errorf("expected zero-value State, got %+v", got)
	}
}

func TestLoadState_CorruptFileTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvStateDirOverride, dir)
	// Write garbage so the JSON unmarshal fails.
	if err := os.WriteFile(filepath.Join(dir, "update.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got == nil || got.LatestVersionSeen != "" {
		t.Errorf("corrupt file should yield empty state, got %+v", got)
	}
}

func TestSaveState_RejectsNil(t *testing.T) {
	t.Setenv(EnvStateDirOverride, t.TempDir())
	if err := SaveState(nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestShouldCheck_TimeBoundary(t *testing.T) {
	now := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		state *State
		want  bool
	}{
		{"nil state always checks", nil, true},
		{"empty state always checks", &State{}, true},
		{"checked 25h ago → check", &State{LastCheckedAt: now.Add(-25 * time.Hour)}, true},
		{"checked exactly 24h ago → check", &State{LastCheckedAt: now.Add(-24 * time.Hour)}, true},
		{"checked 23h ago → skip", &State{LastCheckedAt: now.Add(-23 * time.Hour)}, false},
		{"checked just now → skip", &State{LastCheckedAt: now}, false},
	}
	for _, c := range cases {
		got := ShouldCheck(c.state, now, 24*time.Hour)
		if got != c.want {
			t.Errorf("%s: ShouldCheck = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSaveState_AtomicallyOverwrites(t *testing.T) {
	t.Setenv(EnvStateDirOverride, t.TempDir())
	first := &State{LastCheckedAt: time.Now(), LatestVersionSeen: "v0.3.3"}
	if err := SaveState(first); err != nil {
		t.Fatal(err)
	}
	second := &State{LastCheckedAt: time.Now(), LatestVersionSeen: "v0.3.4"}
	if err := SaveState(second); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadState()
	if got.LatestVersionSeen != "v0.3.4" {
		t.Errorf("second save should win; got %q", got.LatestVersionSeen)
	}
}
