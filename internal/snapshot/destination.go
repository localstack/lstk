package snapshot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	// ErrRemoteNotSupported is returned for known remote schemes (s3://, oras://, cloud:).
	ErrRemoteNotSupported = errors.New("remote destinations are not yet supported — coming soon")
	// ErrUnknownScheme is returned for unrecognized URL schemes.
	ErrUnknownScheme = errors.New("unrecognized destination scheme")
)

// ParseDestination resolves the user-supplied path to an absolute local path.
// When dest is empty, a default name based on now (UTC) is used, e.g.
// "snapshot-2026-05-11T21-04-32.zip", saved in the current working directory.
// The returned path always has a .zip extension.
func ParseDestination(dest string, now time.Time) (string, error) {
	if dest == "" {
		dest = "./" + now.UTC().Format("snapshot-2006-01-02T15-04-05")
	} else {
		lower := strings.ToLower(dest)
		switch {
		case strings.HasPrefix(lower, "s3://"),
			strings.HasPrefix(lower, "oras://"),
			strings.HasPrefix(lower, "cloud:"):
			return "", ErrRemoteNotSupported
		case strings.Contains(lower, "://"):
			scheme := dest[:strings.Index(lower, "://")+3]
			return "", fmt.Errorf("%w: %q", ErrUnknownScheme, scheme)
		}
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

	parent := filepath.Dir(abs)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("parent directory %q does not exist — create it first", parent)
		}
		return "", fmt.Errorf("check parent directory: %w", err)
	}
	if !parentInfo.IsDir() {
		return "", fmt.Errorf("parent path %q is not a directory", parent)
	}

	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return "", fmt.Errorf("%q is a directory — specify a file path like ./my-snapshot", abs)
	}

	if !strings.EqualFold(filepath.Ext(abs), ".zip") {
		abs += ".zip"
	}

	return abs, nil
}
