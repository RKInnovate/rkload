package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvStateDirOverride lets tests redirect the state directory. In
// production the state lives at ~/.rkload/ (sibling of cache/).
const EnvStateDirOverride = "RKLOAD_STATE_DIR"

// stateFile is the on-disk name for the update-check state. Kept
// short and obviously update-related so a curious user can `cat`
// it without surprise.
const stateFile = "update.json"

// State records what the last update check found and when it
// happened. Empty defaults are valid — a fresh install with no
// prior check looks identical to "last check was decades ago".
type State struct {
	LastCheckedAt     time.Time `json:"last_checked_at"`
	LatestVersionSeen string    `json:"latest_version_seen,omitempty"`
}

// StateDir returns the directory the state file lives in. Override
// it via RKLOAD_STATE_DIR for tests; production falls back to
// ~/.rkload/.
func StateDir() (string, error) {
	if override := os.Getenv(EnvStateDirOverride); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("updater: locating home directory: %w", err)
	}
	return filepath.Join(home, ".rkload"), nil
}

// LoadState reads the persisted state, returning a zero-value
// State if the file does not exist (so callers don't need a
// special case for first-run).
func LoadState() (*State, error) {
	dir, err := StateDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupted state file is treated as "no state" — better to
		// silently re-check than to fail every future startup.
		return &State{}, nil
	}
	return &s, nil
}

// SaveState writes State atomically (tmp + rename). The directory
// is created on demand so first-run doesn't need a separate setup
// step.
func SaveState(s *State) error {
	if s == nil {
		return errors.New("updater: SaveState called with nil state")
	}
	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("updater: creating %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, stateFile+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, stateFile))
}

// ShouldCheck reports whether the next discovery call should hit
// the network. interval is exposed so callers can override the
// daily default in tests (and a future flag could open it up).
func ShouldCheck(state *State, now time.Time, interval time.Duration) bool {
	if state == nil {
		return true
	}
	return now.Sub(state.LastCheckedAt) >= interval
}
