package snapshot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParseDestination resolves the user-supplied path to an absolute local path,
// or returns an error for cloud/bare names.
func ParseDestination(dest string) (string, error) {
	if dest == "" {
		dest = "ls-state-export"
	} else if strings.Contains(dest, "://") {
		return "", fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
	} else if !strings.HasPrefix(dest, ".") && !strings.HasPrefix(dest, "~") && !filepath.IsAbs(dest) && filepath.Base(dest) == dest {
		// bare name with no path separators: reserved for future cloud pod names
		return "", fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
	}
	if strings.HasPrefix(dest, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dest = home + dest[1:]
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return abs, nil
}
