package snapshot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrRemoteNotSupported is returned for known remote schemes (s3://, oras://).
	ErrRemoteNotSupported = errors.New("remote destinations are not yet supported — coming soon")
	// ErrUnknownScheme is returned for unrecognized URL schemes.
	ErrUnknownScheme = errors.New("unrecognized destination scheme")

	validPodName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)
)

// DestinationKind distinguishes local file paths from remote pod destinations.
type DestinationKind int

const (
	KindLocal DestinationKind = iota
	KindPod
)

// Destination is the parsed result of a user-supplied snapshot destination.
// For KindLocal, Value is an absolute local file path with a .zip extension.
// For KindPod, Value is the validated pod name (without the "pod:" prefix).
type Destination struct {
	Kind  DestinationKind
	Value string
}

// displayPath shortens abs for human-readable output:
// under cwd → ./rel, under home → ~/rel, otherwise unchanged.
func displayPath(abs, cwd, home string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return "./" + filepath.ToSlash(rel)
		}
	}
	if home != "" {
		if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return abs
}

// ParseDestination resolves a user-supplied destination to a local path (KindLocal) or validated pod name (KindPod).
func ParseDestination(dest string, now time.Time) (Destination, error) {
	if dest == "" {
		b := make([]byte, 2)
		_, _ = rand.Read(b)
		dest = "./" + now.UTC().Format("snapshot-2006-01-02T15-04-05") + "-" + fmt.Sprintf("%x", b)[:3]
	} else {
		lower := strings.ToLower(dest)
		switch {
		case strings.HasPrefix(lower, "pod://"):
			podName := dest[len("pod://"):]
			return Destination{}, fmt.Errorf("'%s' is not a valid reference. Aliases use a single colon. Did you mean:\npod:%s", dest, podName)
		case strings.HasPrefix(lower, "pod:"):
			podName := dest[len("pod:"):]
			if !validPodName.MatchString(podName) {
				return Destination{}, fmt.Errorf("invalid pod name %q: use letters, digits, and hyphens only, starting with a letter or digit", podName)
			}
			return Destination{Kind: KindPod, Value: podName}, nil
		case strings.HasPrefix(lower, "s3://"),
			strings.HasPrefix(lower, "oras://"):
			return Destination{}, ErrRemoteNotSupported
		case strings.Contains(lower, "://"):
			scheme, _, _ := strings.Cut(dest, "://")
			return Destination{}, fmt.Errorf("%w: %q", ErrUnknownScheme, scheme+"://")
		}
	}

	if dest == "~" || strings.HasPrefix(dest, "~/") || strings.HasPrefix(dest, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return Destination{}, fmt.Errorf("resolve home directory: %w", err)
		}
		dest = filepath.Join(home, strings.TrimLeft(dest[1:], `/\`))
	}

	abs, err := filepath.Abs(dest)
	if err != nil {
		return Destination{}, fmt.Errorf("resolve path: %w", err)
	}

	parent := filepath.Dir(abs)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return Destination{}, fmt.Errorf("parent directory %q does not exist — create it first", parent)
		}
		return Destination{}, fmt.Errorf("check parent directory: %w", err)
	}
	if !parentInfo.IsDir() {
		return Destination{}, fmt.Errorf("parent path %q is not a directory", parent)
	}

	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return Destination{}, fmt.Errorf("%q is a directory — specify a file path like ./my-snapshot", abs)
	}

	if !strings.EqualFold(filepath.Ext(abs), ".zip") {
		abs += ".zip"
	}

	return Destination{Kind: KindLocal, Value: abs}, nil
}
