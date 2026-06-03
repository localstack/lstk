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

// ErrHomeNotSet is returned when a path needs "~" expansion but no home directory was provided.
var ErrHomeNotSet = errors.New("home directory is not set")

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

// ParseRemovable parses a ref for snapshot remove. Only cloud (pod:) refs are accepted;
// local file paths are rejected because the CLI cannot delete local files.
// cwd and home are used to produce a human-readable path in error messages.
func ParseRemovable(ref, cwd, home string) (Destination, error) {
	lower := strings.ToLower(ref)
	if !strings.HasPrefix(lower, "pod:") && !strings.Contains(lower, "://") {
		abs, _ := filepath.Abs(ref)
		if !strings.EqualFold(filepath.Ext(abs), ".zip") {
			abs += ".zip"
		}
		return Destination{}, fmt.Errorf("'%s' resolves to a local file (%s); CLI cannot delete local files", ref, displayPath(abs, cwd, home))
	}
	return ParseSource(ref, home)
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

// ParseSource resolves a user-supplied source REF for loading a snapshot.
// Unlike ParseDestination it never auto-generates a name: REF is required.
// For local paths, the file must exist; if no extension is given, .zip is tried as a fallback.
// home is used to expand a leading "~" or "~/"; pass "" to disable tilde expansion.
func ParseSource(ref, home string) (Destination, error) {
	if ref == "" {
		return Destination{}, fmt.Errorf("REF is required for snapshot load")
	}

	lower := strings.ToLower(ref)
	switch {
	case strings.HasPrefix(lower, "pod://"):
		podName := ref[len("pod://"):]
		return Destination{}, fmt.Errorf("'%s' is not a valid reference. Aliases use a single colon. Did you mean:\npod:%s", ref, podName)
	case strings.HasPrefix(lower, "pod:"):
		podName := ref[len("pod:"):]
		if !validPodName.MatchString(podName) {
			return Destination{}, fmt.Errorf("invalid pod name %q: use letters, digits, and hyphens only, starting with a letter or digit", podName)
		}
		return Destination{Kind: KindPod, Value: podName}, nil
	case strings.HasPrefix(lower, "s3://"),
		strings.HasPrefix(lower, "oras://"):
		return Destination{}, ErrRemoteNotSupported
	case strings.Contains(lower, "://"):
		scheme, _, _ := strings.Cut(ref, "://")
		return Destination{}, fmt.Errorf("%w: %q", ErrUnknownScheme, scheme+"://")
	}

	if ref == "~" || strings.HasPrefix(ref, "~/") || strings.HasPrefix(ref, `~\`) {
		if home == "" {
			return Destination{}, fmt.Errorf("cannot expand %q: %w", ref, ErrHomeNotSet)
		}
		ref = filepath.Join(home, strings.TrimLeft(ref[1:], `/\`))
	}

	abs, err := filepath.Abs(ref)
	if err != nil {
		return Destination{}, fmt.Errorf("resolve path: %w", err)
	}

	// Try the path as-is first, then with .zip appended as a fallback for bare
	// names (e.g. "my-snapshot" → "my-snapshot.zip" since that is what save produces).
	resolved, err := resolveSourcePath(abs)
	if err != nil {
		return Destination{}, err
	}
	return Destination{Kind: KindLocal, Value: resolved}, nil
}

// resolveSourcePath returns the first existing path among: abs as-is, then abs+".zip".
func resolveSourcePath(abs string) (string, error) {
	if _, err := os.Stat(abs); err == nil {
		return abs, nil
	}
	withZip := abs + ".zip"
	if _, err := os.Stat(withZip); err == nil {
		return withZip, nil
	}
	return "", fmt.Errorf("snapshot file not found: %q (also tried %q)", abs, withZip)
}

// ParseDestination resolves a user-supplied destination to a local path (KindLocal) or validated pod name (KindPod).
// home is used to expand a leading "~" or "~/"; pass "" to disable tilde expansion.
func ParseDestination(dest, home string, now time.Time) (Destination, error) {
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
		if home == "" {
			return Destination{}, fmt.Errorf("cannot expand %q: %w", dest, ErrHomeNotSet)
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
