// Package cache stores records of which rkload configs have been
// validated, keyed by a canonical hash of their JSON content.
//
// On disk, each entry lives at $RKLOAD_CACHE_DIR/<hash>.json (defaults
// to ~/.rkload/cache/). The cache is a record-keeping aid — its purpose
// is observability and skipping redundant re-validation, not security.
// Treating any cache failure as fatal would couple every load test to
// the user's $HOME being writable, so all errors here are recoverable:
// callers can log them and proceed.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvDirOverride lets tests (and power users) redirect the cache to an
// arbitrary directory. When unset, Dir() falls back to ~/.rkload/cache.
const EnvDirOverride = "RKLOAD_CACHE_DIR"

// StatusValid is the only status currently written. Reserved as a
// constant so future statuses (e.g. "deprecated-schema") have a place
// to land without restructuring the entry shape.
const StatusValid = "valid"

// Entry records a single validated config. Field names match what gets
// serialized to disk; the JSON representation is the contract.
type Entry struct {
	Hash           string         `json:"hash"`
	ValidatedAt    time.Time      `json:"validated_at"`
	RkloadVersion  string         `json:"rkload_version"`
	ConfigPath     string         `json:"config_path"`
	FileSizeBytes  int64          `json:"file_size_bytes"`
	SchemaURL      string         `json:"schema_url,omitempty"`
	SchemaVersion  int            `json:"schema_version"`
	EndpointCounts map[string]int `json:"endpoint_counts"`
	Status         string         `json:"status"`
}

// CanonicalHash returns a deterministic SHA-256 of the JSON value in
// data. The input is unmarshalled into interface{} and re-marshalled,
// which sorts object keys (Go's encoding/json does this for
// map[string]interface{}) and collapses incidental whitespace
// differences. Arrays keep their original order.
//
// Two configs that differ only in formatting or key order therefore
// produce the same hash; any semantic change (different value, added
// or removed field) produces a different hash.
func CanonicalHash(data []byte) (string, error) {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("cache: parsing config for hashing: %w", err)
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("cache: canonicalising config: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// Dir returns the directory where cache entries live. The directory is
// not created here — callers that intend to write should call EnsureDir.
func Dir() (string, error) {
	if override := os.Getenv(EnvDirOverride); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cache: locating home directory: %w", err)
	}
	return filepath.Join(home, ".rkload", "cache"), nil
}

// EnsureDir creates the cache directory (and any parents) if it does
// not already exist. Returns the resolved path on success.
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cache: creating %s: %w", dir, err)
	}
	return dir, nil
}

// entryPath joins Dir() with the conventional <hash>.json filename.
func entryPath(hash string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hash+".json"), nil
}

// Lookup returns the cache entry for hash, or (nil, nil) if no entry
// exists. A corrupted or hash-mismatched file is treated as a miss
// (not an error) so a hand-edited cache directory can't break runs.
func Lookup(hash string) (*Entry, error) {
	path, err := entryPath(hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("cache: reading %s: %w", path, err)
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, nil
	}
	if e.Hash != hash {
		return nil, nil
	}
	return &e, nil
}

// Store writes entry to disk atomically (tmp file + rename) so a
// crashed write never leaves a half-formed cache file behind.
func Store(entry *Entry) error {
	if entry == nil {
		return errors.New("cache: Store called with nil entry")
	}
	if entry.Hash == "" {
		return errors.New("cache: Store called with empty hash")
	}
	dir, err := EnsureDir()
	if err != nil {
		return err
	}
	final := filepath.Join(dir, entry.Hash+".json")

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("cache: encoding entry: %w", err)
	}

	tmp, err := os.CreateTemp(dir, entry.Hash+".*.tmp")
	if err != nil {
		return fmt.Errorf("cache: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("cache: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cache: closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("cache: renaming temp file: %w", err)
	}
	return nil
}
