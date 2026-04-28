package snapshot

import (
	"fmt"
	"io"
	"os"
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
		return LocalDestination{Path: "ls-state-export"}, nil
	}
	if strings.Contains(dest, "://") {
		return nil, fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
	}
	if strings.HasPrefix(dest, ".") || strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "~") || strings.Contains(dest, "/") {
		return LocalDestination{Path: dest}, nil
	}
	// bare name with no path separators: reserved for future cloud pod names
	return nil, fmt.Errorf("cloud destinations are not yet supported — use a file path like ./my-snapshot")
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
