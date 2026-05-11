package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// ---- ArchiveName ---------------------------------------------------------

func TestArchiveName_Combinations(t *testing.T) {
	cases := []struct {
		version, goos, goarch, want string
	}{
		{"v0.3.4", "darwin", "arm64", "rkload_0.3.4_darwin_arm64.tar.gz"},
		{"0.3.4", "darwin", "amd64", "rkload_0.3.4_darwin_x86_64.tar.gz"},
		{"v0.3.4", "linux", "amd64", "rkload_0.3.4_linux_x86_64.tar.gz"},
		{"v0.3.4", "linux", "arm64", "rkload_0.3.4_linux_arm64.tar.gz"},
		{"v0.3.4", "windows", "amd64", "rkload_0.3.4_windows_x86_64.zip"},
		{"v0.3.4", "windows", "arm64", "rkload_0.3.4_windows_arm64.zip"},
		{"v0.3.4", "linux", "386", "rkload_0.3.4_linux_i386.tar.gz"},
	}
	for _, c := range cases {
		got := ArchiveName(c.version, c.goos, c.goarch)
		if got != c.want {
			t.Errorf("ArchiveName(%q,%q,%q) = %q, want %q",
				c.version, c.goos, c.goarch, got, c.want)
		}
	}
}

// ---- AssetURL ------------------------------------------------------------

func TestAssetURL_PrefersAPIDiscoveredURL(t *testing.T) {
	rel := Release{
		Tag:    "v0.3.4",
		Assets: map[string]string{"rkload_0.3.4_darwin_arm64.tar.gz": "https://cdn.example.com/d"},
	}
	if got := AssetURL(rel, "rkload_0.3.4_darwin_arm64.tar.gz"); got != "https://cdn.example.com/d" {
		t.Errorf("AssetURL = %q, want CDN URL from Assets map", got)
	}
}

func TestAssetURL_SynthesizesWhenAssetsMissing(t *testing.T) {
	rel := Release{Tag: "v0.3.4"} // no Assets, simulating redirect-only discovery
	got := AssetURL(rel, "rkload_0.3.4_linux_x86_64.tar.gz")
	if !strings.HasSuffix(got, "/releases/download/v0.3.4/rkload_0.3.4_linux_x86_64.tar.gz") {
		t.Errorf("synthesised URL = %q, want canonical /releases/download/ path", got)
	}
}

// ---- findChecksum --------------------------------------------------------

const sampleChecksums = `a1b2c3  rkload_0.3.4_darwin_arm64.tar.gz
4d5e6f  rkload_0.3.4_darwin_x86_64.tar.gz
deadbe  rkload_0.3.4_linux_x86_64.tar.gz
`

func TestFindChecksum_FoundEntry(t *testing.T) {
	got, err := findChecksum([]byte(sampleChecksums), "rkload_0.3.4_linux_x86_64.tar.gz")
	if err != nil {
		t.Fatalf("findChecksum: %v", err)
	}
	if got != "deadbe" {
		t.Errorf("hash = %q, want deadbe", got)
	}
}

func TestFindChecksum_MissingEntry(t *testing.T) {
	_, err := findChecksum([]byte(sampleChecksums), "rkload_0.3.4_solaris_sparc.tar.gz")
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
	if !strings.Contains(err.Error(), "no entry for") {
		t.Errorf("error should mention missing entry, got: %v", err)
	}
}

func TestFindChecksum_ToleratesBlankLines(t *testing.T) {
	mixed := "\n  \n" + sampleChecksums + "\n"
	got, err := findChecksum([]byte(mixed), "rkload_0.3.4_darwin_arm64.tar.gz")
	if err != nil || got != "a1b2c3" {
		t.Errorf("got %q, %v; want a1b2c3, nil", got, err)
	}
}

// ---- DownloadAndVerify ---------------------------------------------------

// fakeRelease returns a test server that serves the given archive
// bytes and a checksums.txt with the matching SHA-256.
func fakeRelease(t *testing.T, archiveName string, archive []byte, mutate func(checksums string) string) string {
	t.Helper()
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), archiveName)
	if mutate != nil {
		checksums = mutate(checksums)
	}
	srv := withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	})
	return srv.URL
}

func TestDownloadAndVerify_HappyPath(t *testing.T) {
	archive := []byte("imagine this is a tarball")
	archiveName := "rkload_0.3.4_darwin_arm64.tar.gz"
	fakeRelease(t, archiveName, archive, nil)

	rel := Release{Tag: "v0.3.4"} // synthesised URLs
	path, err := DownloadAndVerify(nil, rel, archiveName)
	if err != nil {
		t.Fatalf("DownloadAndVerify: %v", err)
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded: %v", err)
	}
	if string(data) != string(archive) {
		t.Errorf("downloaded bytes = %q, want %q", data, archive)
	}
}

func TestDownloadAndVerify_ChecksumMismatch(t *testing.T) {
	archive := []byte("real archive")
	archiveName := "rkload_0.3.4_linux_x86_64.tar.gz"
	// Mutate checksums to advertise a fake hash → real archive will
	// fail verification.
	fakeRelease(t, archiveName, archive, func(_ string) string {
		return "0000000000000000000000000000000000000000000000000000000000000000  " + archiveName + "\n"
	})

	rel := Release{Tag: "v0.3.4"}
	path, err := DownloadAndVerify(nil, rel, archiveName)
	if err == nil {
		_ = os.Remove(path)
		t.Fatal("expected checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should mention mismatch, got: %v", err)
	}
	// Partial download should have been cleaned up.
	if path != "" {
		if _, statErr := os.Stat(path); statErr == nil {
			t.Errorf("downloaded file should be cleaned up on error: %s", path)
		}
	}
}

func TestDownloadAndVerify_ChecksumsMissingFile(t *testing.T) {
	archive := []byte("archive bytes")
	archiveName := "rkload_0.3.4_linux_x86_64.tar.gz"
	// Serve checksums.txt that omits our archive.
	fakeRelease(t, archiveName, archive, func(_ string) string {
		return "deadbeef  rkload_0.3.4_other_arch.tar.gz\n"
	})

	rel := Release{Tag: "v0.3.4"}
	_, err := DownloadAndVerify(nil, rel, archiveName)
	if err == nil {
		t.Fatal("expected error for missing checksum entry")
	}
	if !strings.Contains(err.Error(), "no entry for") {
		t.Errorf("error should mention missing entry, got: %v", err)
	}
}

func TestDownloadAndVerify_ArchiveNotFound(t *testing.T) {
	// No fakeRelease setup → server is the default httptest 404.
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	rel := Release{Tag: "v0.3.4"}
	_, err := DownloadAndVerify(nil, rel, "rkload_0.3.4_linux_x86_64.tar.gz")
	if err == nil {
		t.Fatal("expected error when archive doesn't exist")
	}
}

func TestDownloadAndVerify_ChecksumsNotFound(t *testing.T) {
	archive := []byte("archive bytes")
	archiveName := "rkload_0.3.4_linux_x86_64.tar.gz"
	withFakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, archiveName) {
			_, _ = w.Write(archive)
			return
		}
		http.NotFound(w, r)
	})

	rel := Release{Tag: "v0.3.4"}
	_, err := DownloadAndVerify(nil, rel, archiveName)
	if err == nil {
		t.Fatal("expected error when checksums.txt is missing")
	}
	if !strings.Contains(err.Error(), "checksums.txt") {
		t.Errorf("error should mention checksums.txt, got: %v", err)
	}
}
