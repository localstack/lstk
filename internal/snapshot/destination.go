package snapshot

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Destination is where snapshot state is written.
type Destination interface {
	Writer() (io.WriteCloser, error)
	String() string
}

// ParseDestination returns a Destination for the user-supplied path, or an error for cloud/bare names.
func ParseDestination(dest string) (Destination, error) {
	if dest == "" {
		dest = "ls-state-export"
	} else if strings.Contains(dest, "://") {
		return nil, fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
	} else if !strings.HasPrefix(dest, ".") && !strings.HasPrefix(dest, "~") && !filepath.IsAbs(dest) && filepath.Base(dest) == dest {
		// bare name with no path separators: reserved for future cloud pod names
		return nil, fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
	}
	if strings.HasPrefix(dest, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		dest = home + dest[1:]
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	return LocalDestination{Path: abs}, nil
}

// LocalDestination writes snapshot state to a local file.
type LocalDestination struct {
	Path string
}

func (d LocalDestination) Writer() (io.WriteCloser, error) {
	return os.Create(d.Path)
}

func (d LocalDestination) String() string {
	return d.Path
}
