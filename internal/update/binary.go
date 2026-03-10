package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
)

func updateBinary(ctx context.Context, tag string) error {
	ver := normalizeVersion(tag)
	assetName := buildAssetName(ver, goruntime.GOOS, goruntime.GOARCH)

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", githubRepo, tag, assetName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(exe), "lstk-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("download write failed: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if goruntime.GOOS == "windows" {
		return extractAndReplace(tmpPath, exe, "zip")
	}
	return extractAndReplace(tmpPath, exe, "tar.gz")
}

func buildAssetName(ver, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("lstk_%s_%s_%s.%s", ver, goos, goarch, ext)
}
