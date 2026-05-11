package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// binarySuffix returns ".exe" on Windows, "" elsewhere — so cross-
// platform callers can construct the in-archive filename without
// caring about the host OS.
func binarySuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// ReplaceSelf atomically swaps the binary at executablePath for the
// rkload binary extracted from archivePath (a GoReleaser .tar.gz or
// .zip).
//
// Unix: os.Rename works on running binaries — the kernel keeps the
// old inode alive for the running process, and only new exec() calls
// see the replacement. Standard, well-supported.
//
// Windows: can't overwrite a running .exe, but *can* rename it. We
// move the running binary to <path>.old first, then rename the
// extracted binary into place. The .old file lingers until the next
// rkload run, when CleanupStaleOld removes it.
//
// Symlinks are resolved before replacement so swapping out
// /usr/local/bin/rkload (often a symlink) replaces the real file
// rather than breaking the symlink.
func ReplaceSelf(executablePath, archivePath string) error {
	resolved, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", executablePath, err)
	}

	binaryName := "rkload" + binarySuffix()
	newPath := resolved + ".new"

	if err := extractBinary(archivePath, binaryName, newPath); err != nil {
		return fmt.Errorf("extracting %s from %s: %w", binaryName, archivePath, err)
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("chmod new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		oldPath := resolved + ".old"
		_ = os.Remove(oldPath) // remove any stale .old from a prior swap
		if err := os.Rename(resolved, oldPath); err != nil {
			_ = os.Remove(newPath)
			return fmt.Errorf("renaming current binary aside: %w", err)
		}
		if err := os.Rename(newPath, resolved); err != nil {
			// Roll back: put the old binary back so the user isn't
			// left with no binary at all.
			_ = os.Rename(oldPath, resolved)
			_ = os.Remove(newPath)
			return fmt.Errorf("renaming new binary into place: %w", err)
		}
		return nil
	}

	if err := os.Rename(newPath, resolved); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("renaming new binary into place: %w", err)
	}
	return nil
}

// CleanupStaleOld removes a leftover <executablePath>.old file from
// a previous Windows update. Safe to call on any platform — non-
// existence is silently ignored. Intended to run at startup.
func CleanupStaleOld(executablePath string) {
	resolved, err := filepath.EvalSymlinks(executablePath)
	if err != nil {
		return
	}
	_ = os.Remove(resolved + ".old")
}

// extractBinary dispatches to the right extractor based on the
// archive's extension. We don't sniff content — GoReleaser only
// publishes these two formats and the filename is authoritative.
func extractBinary(archivePath, binaryName, outPath string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, binaryName, outPath)
	}
	return extractFromTarGz(archivePath, binaryName, outPath)
}

func extractFromTarGz(archivePath, binaryName, outPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary %q not found in archive", binaryName)
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		return writeTo(outPath, tr)
	}
}

func extractFromZip(archivePath, binaryName, outPath string) error {
	z, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer z.Close()

	for _, entry := range z.File {
		if filepath.Base(entry.Name) != binaryName {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeTo(outPath, rc)
	}
	return fmt.Errorf("binary %q not found in archive", binaryName)
}

// writeTo creates outPath (truncating any existing file) and copies
// r into it. Centralised here so the tar.gz and zip extractors share
// identical write behaviour, including 0755 mode on the new file.
func writeTo(outPath string, r io.Reader) error {
	out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		_ = os.Remove(outPath)
		return err
	}
	return out.Close()
}
