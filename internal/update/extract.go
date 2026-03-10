package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
)

func extractAndReplace(archivePath, exePath, format string) error {
	dir, err := os.MkdirTemp("", "lstk-extract-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	switch format {
	case "tar.gz":
		if err := extractTarGz(archivePath, dir); err != nil {
			return fmt.Errorf("extract failed: %w", err)
		}
	case "zip":
		if err := extractZip(archivePath, dir); err != nil {
			return fmt.Errorf("extract failed: %w", err)
		}
	}

	binaryName := "lstk"
	if goruntime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}

	newBinary := filepath.Join(dir, binaryName)
	if _, err := os.Stat(newBinary); err != nil {
		return fmt.Errorf("binary not found in archive: %w", err)
	}

	info, err := os.Stat(exePath)
	if err != nil {
		return err
	}

	// On Windows, a running executable cannot be overwritten but can be renamed.
	// Move it out of the way first so we can place the new binary at the original path.
	if goruntime.GOOS == "windows" {
		oldPath := exePath + ".old"
		// Clean up leftover from a previous update; ignore error if it doesn't exist.
		if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove old binary %s: %w", oldPath, err)
		}
		if err := os.Rename(exePath, oldPath); err != nil {
			return fmt.Errorf("cannot move running binary: %w", err)
		}
	}

	if err := os.Rename(newBinary, exePath); err != nil {
		// Cross-device rename: fall back to copy
		return copyFile(newBinary, exePath, info.Mode())
	}

	return os.Chmod(exePath, info.Mode())
}

func extractTarGz(archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, filepath.Clean(hdr.Name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, hdr.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		}
	}
	return nil
}

func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		target := filepath.Join(destDir, filepath.Clean(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			_ = rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			_ = rc.Close()
			return err
		}
		_ = out.Close()
		_ = rc.Close()
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
