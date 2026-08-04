package snapshot

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	// ErrPodVersionNotSupported is returned when a pod ref carries a ":<version>"
	// suffix in a context that can only ever address the latest version (save,
	// show, remove, versions, and S3 remotes).
	ErrPodVersionNotSupported = errors.New("a specific snapshot version is not supported here")
	// ErrVersionsRemoteUnsupported is returned when `snapshot versions` is given an
	// s3:// ref. Version history comes from the LocalStack platform, which tracks it
	// for pod: snapshots only, so this is a scope limit of the command rather than a
	// missing feature — hence not ErrRemoteNotSupported's "coming soon" wording.
	ErrVersionsRemoteUnsupported = errors.New("snapshot versions is only supported for Cloud Pods (pod: refs), not S3 remotes")
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
	// Version is the requested version of a KindPod snapshot; 0 means "latest".
	// It is only ever non-zero for KindPod: local paths are never split on ":"
	// (that would break Windows drive letters) and S3 remotes have no version
	// addressing.
	Version int
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

// ValidateRemotePodName validates a pod name given as a positional alongside an
// s3:// location. S3 remotes have no version addressing, so a ":<version>"
// suffix is rejected with a message that explains why, rather than falling
// through to ValidatePodName's opaque "invalid pod name".
func ValidateRemotePodName(name string) error {
	_, version, err := splitPodRef(name, name)
	if err != nil {
		return err
	}
	if version != 0 {
		return fmt.Errorf("%w: %q — S3 remotes do not support snapshot versions", ErrPodVersionNotSupported, name)
	}
	return ValidatePodName(name)
}

// PodRef renders a pod name and version back into the reference syntax the user
// types: "pod:name" for the latest version, "pod:name:N" for a pinned one.
func PodRef(name string, version int) string {
	if version <= 0 {
		return "pod:" + name
	}
	return "pod:" + name + ":" + strconv.Itoa(version)
}

// splitPodRef splits a pod identifier into a name and an optional ":<version>"
// suffix. A colon is never legal in a pod name (validate.PodName allows only
// letters, digits, hyphens, and underscores), so any colon is unambiguously a
// version separator: an unparseable suffix is reported as a bad version rather
// than folded back into the name. The legacy CLI did the latter, which surfaced
// as a confusing "invalid pod name".
//
// displayRef is what the user actually typed, used only in the error message —
// it is the full "pod:..." ref on the load/save path but a bare name on the S3
// positional path, so it cannot be reconstructed from raw.
func splitPodRef(raw, displayRef string) (name string, version int, err error) {
	i := strings.LastIndex(raw, ":")
	if i < 0 {
		return raw, 0, nil
	}
	name, suffix := raw[:i], raw[i+1:]
	// ParseUint rather than Atoi so a sign prefix ("+3", "-3") is rejected, with
	// a 31-bit limit so an oversized number fails here instead of wrapping.
	// Version 0 does not exist on the platform, which also covers a trailing ":".
	v, perr := strconv.ParseUint(suffix, 10, 31)
	if perr != nil || v == 0 {
		return "", 0, fmt.Errorf("invalid version %q in %q: use a positive integer", suffix, displayRef)
	}
	return name, int(v), nil
}

// parsePodRef parses the shared "pod:" branch of ParseSource and ParseDestination.
// ref includes the "pod:" prefix. allowVersion is false for save, which can only
// ever write a new version (the platform assigns it).
func parsePodRef(ref string, allowVersion bool) (Destination, error) {
	name, version, err := splitPodRef(ref[len("pod:"):], ref)
	if err != nil {
		return Destination{}, err
	}
	if err := ValidatePodName(name); err != nil {
		return Destination{}, err
	}
	if version != 0 && !allowVersion {
		return Destination{}, fmt.Errorf("%w: 'pod:%s:%d' — the platform assigns the version on each save; use 'pod:%s'",
			ErrPodVersionNotSupported, name, version, name)
	}
	return Destination{Kind: KindPod, Value: name, Version: version}, nil
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
// A ":<version>" suffix is rejected: remove deletes the whole pod, so honouring
// only part of the ref would silently destroy more than the user asked for.
// cwd and home are used to produce a human-readable path in error messages.
func ParseRemovable(ref, cwd, home string) (Destination, error) {
	return parseCloudOnly(ref, cwd, home, "delete local files", false)
}

// ParseShowable parses a ref for snapshot show. Only cloud (pod:) refs are accepted;
// local file paths are rejected because show only inspects cloud snapshots.
// A ":<version>" suffix is allowed — show is read-only and every field it renders
// is per-version in the platform response.
// cwd and home are used to produce a human-readable path in error messages.
func ParseShowable(ref, cwd, home string) (Destination, error) {
	return parseCloudOnly(ref, cwd, home, "show local snapshots", true)
}

// parseCloudOnly validates that ref is a cloud (pod:) reference, rejecting local
// file paths with a message naming the unsupported action (e.g. "delete local files").
// allowVersion is true only for read-only commands that can meaningfully report a
// single version (show). It is false where a version would be ignored, which must
// fail loudly rather than silently widen the operation.
func parseCloudOnly(ref, cwd, home, action string, allowVersion bool) (Destination, error) {
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
	// remove/show/versions are cloud (pod:) only; S3 remotes are not yet supported here.
	if dest.Kind == KindS3 {
		return Destination{}, ErrRemoteNotSupported
	}
	// Where a version cannot be honoured it must fail loudly rather than be
	// ignored: silently dropping it would make "remove pod:x:3" delete the whole
	// pod, and would make "versions pod:x:3" look like it filtered the listing.
	if dest.Version != 0 && !allowVersion {
		return Destination{}, fmt.Errorf("%w: drop the ':%d' from %q", ErrPodVersionNotSupported, dest.Version, ref)
	}
	return dest, nil
}

// ParseVersionable parses a ref for snapshot versions. Only cloud (pod:) refs are
// accepted. A ":<version>" suffix is rejected because this command lists every
// version.
//
// An s3:// ref gets its own error rather than parseCloudOnly's
// ErrRemoteNotSupported: that sentinel says "coming soon", which is right for
// oras:// (unimplemented everywhere) but misleading here, since S3 remotes are
// fully supported by save/load/list and it is only version history that is
// pod-only.
func ParseVersionable(ref, cwd, home string) (Destination, error) {
	if IsS3Ref(ref) {
		return Destination{}, ErrVersionsRemoteUnsupported
	}
	return parseCloudOnly(ref, cwd, home, "list versions of local snapshots", false)
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
		return parsePodRef(ref, true)
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
			return parsePodRef(dest, false)
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
