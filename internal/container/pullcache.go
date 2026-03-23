package container

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const pullCacheTTL = 24 * time.Hour

// pullCacheDir returns the directory used to store pull timestamps.
func pullCacheDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "lstk", "pull-timestamps"), nil
}

// sanitizeImageName converts an image reference into a safe filename.
func sanitizeImageName(image string) string {
	r := strings.NewReplacer("/", "-", ":", "-")
	return r.Replace(image)
}

// shouldPull reports whether the given image should be pulled based on
// the cached pull timestamp and the configured TTL.
func shouldPull(image string, ttl time.Duration) bool {
	dir, err := pullCacheDir()
	if err != nil {
		return true
	}
	path := filepath.Join(dir, sanitizeImageName(image))
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > ttl
}

// recordPull writes (or touches) a timestamp file for the given image.
func recordPull(image string) error {
	dir, err := pullCacheDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create pull cache directory: %w", err)
	}
	path := filepath.Join(dir, sanitizeImageName(image))
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to write pull cache file: %w", err)
	}
	return f.Close()
}
