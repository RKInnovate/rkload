package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// buildTarGz returns a tar.gz archive containing one file at the
// given name with the given content. Used to fabricate fake rkload
// release archives for the replace tests.
func buildTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Size: int64(len(content)),
		Mode: 0o755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildZip(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTempFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- ReplaceSelf ---------------------------------------------------------

func TestReplaceSelf_TarGzReplacesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tar.gz path covered on non-Windows; Windows uses zip")
	}
	dir := t.TempDir()
	exe := writeTempFile(t, dir, "rkload", []byte("OLD"))
	archive := writeTempFile(t, dir, "release.tar.gz", buildTarGz(t, "rkload", []byte("NEW")))

	if err := ReplaceSelf(exe, archive); err != nil {
		t.Fatalf("ReplaceSelf: %v", err)
	}
	got, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(got) != "NEW" {
		t.Errorf("binary content = %q, want NEW (replacement did not land)", got)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Errorf(".new staging file should be gone after rename")
	}
}

func TestReplaceSelf_PreservesExecutableBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes don't apply on Windows")
	}
	dir := t.TempDir()
	exe := writeTempFile(t, dir, "rkload", []byte("OLD"))
	archive := writeTempFile(t, dir, "release.tar.gz", buildTarGz(t, "rkload", []byte("NEW")))
	if err := ReplaceSelf(exe, archive); err != nil {
		t.Fatalf("ReplaceSelf: %v", err)
	}
	info, _ := os.Stat(exe)
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("replaced binary lost executable bit: mode = %v", info.Mode().Perm())
	}
}

func TestReplaceSelf_ZipFormat(t *testing.T) {
	// .zip is the windows archive format but the extraction works on
	// any host — exercise it regardless of GOOS so the test runs on
	// our CI matrix.
	dir := t.TempDir()
	exeName := "rkload"
	if runtime.GOOS == "windows" {
		exeName = "rkload.exe"
	}
	exe := writeTempFile(t, dir, exeName, []byte("OLD"))
	archive := writeTempFile(t, dir, "release.zip", buildZip(t, exeName, []byte("NEW")))

	if err := ReplaceSelf(exe, archive); err != nil {
		t.Fatalf("ReplaceSelf: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "NEW" {
		t.Errorf("binary content = %q, want NEW", got)
	}
}

func TestReplaceSelf_BinaryNotInArchive(t *testing.T) {
	dir := t.TempDir()
	exeName := "rkload" + binarySuffix()
	exe := writeTempFile(t, dir, exeName, []byte("OLD"))
	// Archive contains the wrong filename.
	archive := writeTempFile(t, dir, "release.tar.gz", buildTarGz(t, "not-rkload", []byte("NEW")))

	err := ReplaceSelf(exe, archive)
	if err == nil {
		t.Fatal("expected error when binary not present in archive")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD" {
		t.Errorf("binary modified despite failed extract: %q", got)
	}
}

func TestReplaceSelf_NonexistentExecutable(t *testing.T) {
	dir := t.TempDir()
	archive := writeTempFile(t, dir, "release.tar.gz", buildTarGz(t, "rkload", []byte("NEW")))
	err := ReplaceSelf(filepath.Join(dir, "no-such-binary"), archive)
	if err == nil {
		t.Fatal("expected error when executable does not exist")
	}
}

func TestReplaceSelf_NonexistentArchive(t *testing.T) {
	dir := t.TempDir()
	exeName := "rkload" + binarySuffix()
	exe := writeTempFile(t, dir, exeName, []byte("OLD"))
	err := ReplaceSelf(exe, filepath.Join(dir, "no-such-archive.tar.gz"))
	if err == nil {
		t.Fatal("expected error when archive does not exist")
	}
}

func TestReplaceSelf_ResolvesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on stock Windows; skipping")
	}
	dir := t.TempDir()
	real := writeTempFile(t, dir, "rkload-real", []byte("OLD"))
	link := filepath.Join(dir, "rkload")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	archive := writeTempFile(t, dir, "release.tar.gz", buildTarGz(t, "rkload", []byte("NEW")))

	if err := ReplaceSelf(link, archive); err != nil {
		t.Fatalf("ReplaceSelf via symlink: %v", err)
	}
	// The symlink should still be there.
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("stat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced instead of target file")
	}
	got, _ := os.ReadFile(real)
	if string(got) != "NEW" {
		t.Errorf("target file content = %q, want NEW", got)
	}
}

// ---- CleanupStaleOld -----------------------------------------------------

func TestCleanupStaleOld_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	exe := writeTempFile(t, dir, "rkload", []byte("running"))
	stale := writeTempFile(t, dir, "rkload.old", []byte("old binary"))

	CleanupStaleOld(exe)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf(".old file should have been removed; stat err = %v", err)
	}
	// Real binary untouched.
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("running binary touched: %v", err)
	}
}

func TestCleanupStaleOld_NoStaleFileIsFine(t *testing.T) {
	dir := t.TempDir()
	exe := writeTempFile(t, dir, "rkload", []byte("running"))
	// No .old file exists — should be a no-op, no panic.
	CleanupStaleOld(exe)
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("CleanupStaleOld disturbed the running binary: %v", err)
	}
}
