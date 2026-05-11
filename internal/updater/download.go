package updater

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// ArchiveName builds the GoReleaser-style filename for a given
// version + GOOS/GOARCH triple. Matches the name_template in
// .goreleaser.yaml: amd64 → x86_64, 386 → i386, everything else
// passes through; Windows uses .zip, the rest .tar.gz.
//
// Always returns a non-empty string — invalid inputs produce a
// best-effort name that will simply fail to download, surfacing
// the issue at the network layer rather than as a silent miss.
func ArchiveName(version, goos, goarch string) string {
	ver := strings.TrimPrefix(version, "v")
	arch := goarch
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "386":
		arch = "i386"
	}
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("rkload_%s_%s_%s.%s", ver, goos, arch, ext)
}

// AssetURL returns the download URL for a named asset. Prefers the
// API-discovered URL when available, otherwise synthesises the
// canonical GitHub release URL — which always works for public
// repos. This way callers can use redirect-discovered releases
// without losing download access.
func AssetURL(release Release, name string) string {
	if u, ok := release.Assets[name]; ok {
		return u
	}
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", ReleaseRedirectBase, Repo, release.Tag, name)
}

// DownloadAndVerify fetches an archive plus checksums.txt, verifies
// the archive's SHA-256 against the expected value in checksums.txt,
// and returns the local path to the downloaded archive.
//
// On any error — network failure, missing checksum entry, hash
// mismatch — the partial download is removed before returning so
// callers don't have to think about cleanup. On success, the caller
// owns the returned file and is responsible for removing it.
func DownloadAndVerify(client *http.Client, release Release, archiveName string) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: HTTPTimeout}
	}

	archivePath, err := downloadToTemp(client, AssetURL(release, archiveName), archiveName)
	if err != nil {
		return "", fmt.Errorf("downloading archive: %w", err)
	}

	checksums, err := downloadBytes(client, AssetURL(release, "checksums.txt"))
	if err != nil {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("downloading checksums.txt: %w", err)
	}

	expected, err := findChecksum(checksums, archiveName)
	if err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}

	actual, err := sha256File(archivePath)
	if err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}

	if actual != expected {
		_ = os.Remove(archivePath)
		return "", fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archiveName, expected, actual)
	}
	return archivePath, nil
}

// downloadToTemp streams the URL into a temp file with the given
// suggested name (purely for readability when debugging — the temp
// dir randomises the prefix). Returns the path to the closed file.
func downloadToTemp(client *http.Client, url, hint string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rkload-updater")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %d", url, resp.StatusCode)
	}

	f, err := os.CreateTemp("", "rkload-dl-*-"+hint)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// downloadBytes is the small-payload counterpart to downloadToTemp.
// Used for checksums.txt which is small enough to live in memory.
func downloadBytes(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rkload-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// findChecksum extracts the SHA-256 hex digest for a named file from
// a GoReleaser-style checksums.txt — lines look like
//
//	<sha256>  <filename>
//
// (two-space separator is canonical, but we tolerate any whitespace).
// Missing entry returns a clear error so callers can distinguish
// "tampered archive" from "wrong filename".
func findChecksum(checksumsTxt []byte, archiveName string) (string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksumsTxt))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// SplitN(2) so any filename containing spaces stays intact.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Re-join everything after the first field in case the
		// filename had whitespace (it shouldn't, but be permissive).
		name := strings.Join(fields[1:], " ")
		if name == archiveName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading checksums.txt: %w", err)
	}
	return "", fmt.Errorf("checksums.txt has no entry for %q", archiveName)
}

// sha256File computes the hex-encoded SHA-256 of the file at path.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
