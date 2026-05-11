package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrCloudNotSupported = errors.New("cloud destinations are not yet supported — use a file path like ./my-snapshot.zip")

// ParseDestination resolves the user-supplied path to an absolute local path,
// or returns an error for cloud/bare names. When dest is empty, a default name
// based on now (UTC) is used, e.g. "snapshot-2026-05-11T21-04-32.zip".
// The returned path always has a .zip extension.
func ParseDestination(dest string, now time.Time) (string, error) {
	if dest == "" {
		dest = "./" + now.UTC().Format("snapshot-2006-01-02T15-04-05")
	} else if strings.Contains(dest, "://") {
		return "", ErrCloudNotSupported
	} else if !strings.HasPrefix(dest, ".") && !strings.HasPrefix(dest, "~") && !filepath.IsAbs(dest) && filepath.Base(dest) == dest {
		// bare name with no path separators: reserved for future cloud pod names
		return "", ErrCloudNotSupported
	}
	if dest == "~" || strings.HasPrefix(dest, "~/") || strings.HasPrefix(dest, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dest = filepath.Join(home, strings.TrimLeft(dest[1:], `/\`))
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return "", fmt.Errorf("%q is a directory — specify a file path like ./my-snapshot.zip", abs)
	}
	if !strings.EqualFold(filepath.Ext(abs), ".zip") {
		abs += ".zip"
	}
	return abs, nil
}
