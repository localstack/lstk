package snapshot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/validate"
)

// ErrHomeNotSet is returned when a path needs "~" expansion but no home directory was provided.
var ErrHomeNotSet = errors.New("home directory is not set")

var (
	// ErrRemoteNotSupported is returned for remote schemes that are not yet
	// implemented (e.g. oras://). S3 (s3://) is supported.
	ErrRemoteNotSupported = errors.New("remote destinations are not yet supported — coming soon")
	// ErrUnknownScheme is returned for unrecognized URL schemes.
	ErrUnknownScheme = errors.New("unrecognized destination scheme")
	// ErrCredentialsInS3URL is returned when an s3:// ref embeds credential query
	// params. Credentials must come from the environment or --profile, never the URL.
	ErrCredentialsInS3URL = errors.New("do not put credentials in the s3:// URL; use AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY or --profile")
)

const (
	snapshotExt       = ".snapshot" // user-facing extension for local snapshots
	legacySnapshotExt = ".zip"      // accepted on load for backward compatibility
)

// withSnapshotExt forces the .snapshot extension, replacing any other the user gave.
func withSnapshotExt(path string) string {
	ext := filepath.Ext(path)
	if strings.EqualFold(ext, snapshotExt) {
		return path
	}
	return strings.TrimSuffix(path, ext) + snapshotExt
}

// DestinationKind distinguishes local file paths from remote pod destinations.
type DestinationKind int

const (
	KindLocal DestinationKind = iota
	KindPod
	KindS3
)

// Destination is the parsed result of a user-supplied snapshot destination.
// For KindLocal, Value is an absolute local file path with a .snapshot extension.
// For KindPod, Value is the validated pod name (without the "pod:" prefix).
// For KindS3, Value is the validated s3:// URL (bucket + optional key prefix), with
// no credential query params — credentials are supplied separately at runtime.
type Destination struct {
	Kind  DestinationKind
	Value string
}

// IsS3Ref reports whether ref is an s3:// reference. Used at the command boundary
// to classify positional args into a pod name and an S3 location.
func IsS3Ref(ref string) bool {
	return strings.HasPrefix(strings.ToLower(ref), "s3://")
}

// ValidatePodName validates a user-supplied pod name (the identity of a snapshot
// on a remote), using the same rules as pod: refs.
func ValidatePodName(name string) error {
	if err := validate.PodName(name); err != nil {
		return fmt.Errorf("invalid pod name %q: %w", name, err)
	}
	return nil
}

// DefaultRemotePodName generates a timestamped pod name used when saving to a
// remote without an explicit name, mirroring local snapshot auto-naming.
func DefaultRemotePodName(now time.Time) string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return "snapshot-" + now.UTC().Format("2006-01-02T15-04-05") + "-" + fmt.Sprintf("%x", b)[:3]
}

// parseS3 validates an s3:// URL and returns it as a KindS3 destination. The bucket
// must be present and the URL must not contain credential query params.
func parseS3(ref string) (Destination, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return Destination{}, fmt.Errorf("invalid s3:// URL %q: %w", ref, err)
	}
	if u.Host == "" {
		return Destination{}, fmt.Errorf("invalid s3:// URL %q: missing bucket name", ref)
	}
	q := u.Query()
	for _, k := range []string{"access_key_id", "secret_access_key", "session_token"} {
		if q.Has(k) {
			return Destination{}, ErrCredentialsInS3URL
		}
	}
	return Destination{Kind: KindS3, Value: ref}, nil
}

// ParseRemovable parses a ref for snapshot remove. Only cloud (pod:) refs are accepted;
// local file paths are rejected because the CLI cannot delete local files.
// cwd and home are used to produce a human-readable path in error messages.
func ParseRemovable(ref, cwd, home string) (Destination, error) {
	return parseCloudOnly(ref, cwd, home, "delete local files")
}

// ParseShowable parses a ref for snapshot show. Only cloud (pod:) refs are accepted;
// local file paths are rejected because show only inspects cloud snapshots.
// cwd and home are used to produce a human-readable path in error messages.
func ParseShowable(ref, cwd, home string) (Destination, error) {
	return parseCloudOnly(ref, cwd, home, "show local snapshots")
}

// parseCloudOnly validates that ref is a cloud (pod:) reference, rejecting local
// file paths with a message naming the unsupported action (e.g. "delete local files").
func parseCloudOnly(ref, cwd, home, action string) (Destination, error) {
	lower := strings.ToLower(ref)
	if !strings.HasPrefix(lower, "pod:") && !strings.Contains(lower, "://") {
		abs, _ := filepath.Abs(ref)
		abs = withSnapshotExt(abs)
		return Destination{}, fmt.Errorf("'%s' resolves to a local file (%s); CLI cannot %s", ref, displayPath(abs, cwd, home), action)
	}
	dest, err := ParseSource(ref, home)
	if err != nil {
		return Destination{}, err
	}
	// remove/show are cloud (pod:) only; S3 remotes are not yet supported here.
	if dest.Kind == KindS3 {
		return Destination{}, ErrRemoteNotSupported
	}
	return dest, nil
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
// For local paths, the file must exist; if no matching file is found, .snapshot and
// then .zip (legacy) are tried as fallbacks.
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
		if err := ValidatePodName(podName); err != nil {
			return Destination{}, err
		}
		return Destination{Kind: KindPod, Value: podName}, nil
	case strings.HasPrefix(lower, "s3://"):
		return parseS3(ref)
	case strings.HasPrefix(lower, "oras://"):
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

	// Try the path as-is first, then with .snapshot appended as a fallback for bare
	// names (e.g. "my-snapshot" → "my-snapshot.snapshot" since that is what save
	// produces), and finally .zip for snapshots saved by older lstk versions.
	resolved, err := resolveSourcePath(abs)
	if err != nil {
		return Destination{}, err
	}
	return Destination{Kind: KindLocal, Value: resolved}, nil
}

// resolveSourcePath returns the first existing file among: abs as-is, then
// abs+".snapshot", then abs+".zip" (legacy). A directory match is remembered but
// skipped in favor of a later file match, so a directory error only surfaces when
// no candidate resolves to an actual file.
func resolveSourcePath(abs string) (string, error) {
	withSnapshot := abs + snapshotExt
	withZip := abs + legacySnapshotExt
	var dirHit string
	for _, candidate := range []string{abs, withSnapshot, withZip} {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			if dirHit == "" {
				dirHit = candidate
			}
			continue
		}
		return candidate, nil
	}
	if dirHit != "" {
		return "", fmt.Errorf("%q is a directory — specify a snapshot file, e.g. ./my-snapshot.snapshot", dirHit)
	}
	return "", fmt.Errorf("snapshot file not found: %q (also tried %q and %q)", abs, withSnapshot, withZip)
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
			if err := ValidatePodName(podName); err != nil {
				return Destination{}, err
			}
			return Destination{Kind: KindPod, Value: podName}, nil
		case strings.HasPrefix(lower, "s3://"):
			return parseS3(dest)
		case strings.HasPrefix(lower, "oras://"):
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

	abs = withSnapshotExt(abs)

	return Destination{Kind: KindLocal, Value: abs}, nil
}
