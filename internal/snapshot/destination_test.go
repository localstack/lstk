package snapshot_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSourcePodVersion covers the Docker-like "pod:<name>:<version>" load
// grammar. A colon is never legal in a pod name, so an unparseable suffix must
// report a bad version rather than being folded back into the name (which is
// what the legacy CLI did, surfacing as a confusing "invalid pod name").
func TestParseSourcePodVersion(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	t.Run("accepted", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			input       string
			wantPodName string
			wantVersion int
		}{
			{"pod:my-baseline", "my-baseline", 0},
			{"pod:my-baseline:1", "my-baseline", 1},
			{"pod:my-baseline:3", "my-baseline", 3},
			{"pod:my-baseline:42", "my-baseline", 42},
			{"pod:my_pod-2:7", "my_pod-2", 7},
		} {
			t.Run(tc.input, func(t *testing.T) {
				t.Parallel()
				dest, err := snapshot.ParseSource(tc.input, home)
				require.NoError(t, err)
				assert.Equal(t, snapshot.KindPod, dest.Kind)
				assert.Equal(t, tc.wantPodName, dest.Value)
				assert.Equal(t, tc.wantVersion, dest.Version)
			})
		}
	})

	t.Run("rejected as a bad version", func(t *testing.T) {
		t.Parallel()
		// Every one of these has a colon, so the suffix is unambiguously meant as
		// a version and the error must say so — not "invalid pod name".
		for _, input := range []string{
			"pod:my-baseline:abc",
			"pod:my-baseline:",
			"pod:my-baseline:0",
			"pod:my-baseline:-1",
			"pod:my-baseline:+3",
			"pod:my-baseline:3.0",
			"pod:my-baseline: 3",
			"pod:my-baseline:99999999999999999999", // beyond the 31-bit limit
		} {
			t.Run(input, func(t *testing.T) {
				t.Parallel()
				_, err := snapshot.ParseSource(input, home)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid version")
				assert.NotContains(t, err.Error(), "invalid pod name")
			})
		}
	})

	t.Run("only the last colon separates the version", func(t *testing.T) {
		t.Parallel()
		// "a:b" is not a legal pod name, so this fails validation on the name —
		// the version itself parsed fine.
		_, err := snapshot.ParseSource("pod:a:b:3", home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pod name")
	})

	t.Run("pod:// hint still wins over version parsing", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseSource("pod://my-baseline:3", home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "single colon")
	})
}

// TestParseSourceLocalPathWithColon guards the version split against escaping
// the "pod:" branch — a Windows drive letter or a colon in a filename must never
// be read as a version.
func TestParseSourceLocalPathWithColon(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("colons are not legal in Windows filenames")
	}
	dir := t.TempDir()
	weird := filepath.Join(dir, "snap:2.snapshot")
	require.NoError(t, os.WriteFile(weird, []byte("data"), 0600))

	dest, err := snapshot.ParseSource(weird, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, snapshot.KindLocal, dest.Kind)
	assert.Equal(t, weird, dest.Value)
	assert.Zero(t, dest.Version, "a local path must never be split on a colon")
}

// TestParseDestinationRejectsPodVersion: saving always creates a new version, so
// a pinned destination is meaningless. The message must explain that rather than
// falling through to "invalid pod name".
func TestParseDestinationRejectsPodVersion(t *testing.T) {
	t.Parallel()
	_, err := snapshot.ParseDestination("pod:my-baseline:3", t.TempDir(), time.Now())
	require.Error(t, err)
	assert.ErrorIs(t, err, snapshot.ErrPodVersionNotSupported)
	assert.Contains(t, err.Error(), "pod:my-baseline")
	assert.NotContains(t, err.Error(), "invalid pod name")
}

// TestParseCloudOnlyRejectsPodVersion: remove and versions operate on a whole
// pod, so a version they cannot honour must fail rather than be ignored —
// otherwise "remove pod:x:3" silently deletes every version of the pod, and
// "versions pod:x:3" looks like it filtered the listing.
func TestParseCloudOnlyRejectsPodVersion(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	for name, parse := range map[string]func(string, string, string) (snapshot.Destination, error){
		"remove":   snapshot.ParseRemovable,
		"versions": snapshot.ParseVersionable,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parse("pod:my-baseline:3", cwd, home)
			require.Error(t, err)
			assert.ErrorIs(t, err, snapshot.ErrPodVersionNotSupported)
		})
	}
}

// TestParseShowableAcceptsPodVersion: show is read-only and every field it
// renders is per-version in the platform response, so it can address one
// directly.
func TestParseShowableAcceptsPodVersion(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	dest, err := snapshot.ParseShowable("pod:my-baseline:3", cwd, home)
	require.NoError(t, err)
	assert.Equal(t, snapshot.KindPod, dest.Kind)
	assert.Equal(t, "my-baseline", dest.Value)
	assert.Equal(t, 3, dest.Version)

	// Still rejects a malformed version rather than folding it into the name.
	_, err = snapshot.ParseShowable("pod:my-baseline:abc", cwd, home)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version")
}

func TestParseVersionable(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	t.Run("accepts pod ref", func(t *testing.T) {
		t.Parallel()
		dest, err := snapshot.ParseVersionable("pod:my-baseline", cwd, home)
		require.NoError(t, err)
		assert.Equal(t, snapshot.KindPod, dest.Kind)
		assert.Equal(t, "my-baseline", dest.Value)
	})

	t.Run("rejects local path", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseVersionable("./my-snapshot", cwd, home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "list versions of local snapshots")
	})

	t.Run("rejects s3 remote", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseVersionable("s3://bucket/prefix", cwd, home)
		require.Error(t, err)
		assert.ErrorIs(t, err, snapshot.ErrRemoteNotSupported)
	})

	t.Run("rejects invalid pod name", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseVersionable("pod:bad name", cwd, home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pod name")
	})
}

func TestValidateRemotePodName(t *testing.T) {
	t.Parallel()

	t.Run("accepts a plain name", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, snapshot.ValidateRemotePodName("my-pod"))
	})

	t.Run("rejects a version suffix", func(t *testing.T) {
		t.Parallel()
		err := snapshot.ValidateRemotePodName("my-pod:3")
		require.Error(t, err)
		assert.ErrorIs(t, err, snapshot.ErrPodVersionNotSupported)
		assert.Contains(t, err.Error(), "S3 remotes do not support snapshot versions")
	})

	t.Run("reports a malformed version as a bad version", func(t *testing.T) {
		t.Parallel()
		err := snapshot.ValidateRemotePodName("my-pod:abc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid version")
	})

	t.Run("rejects an invalid name", func(t *testing.T) {
		t.Parallel()
		err := snapshot.ValidateRemotePodName("bad name")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pod name")
	})
}

func TestPodRef(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pod:my-baseline", snapshot.PodRef("my-baseline", 0))
	assert.Equal(t, "pod:my-baseline", snapshot.PodRef("my-baseline", -1))
	assert.Equal(t, "pod:my-baseline:3", snapshot.PodRef("my-baseline", 3))
}

func TestParseShowable(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	t.Run("accepts pod ref", func(t *testing.T) {
		t.Parallel()
		dest, err := snapshot.ParseShowable("pod:my-baseline", cwd, home)
		require.NoError(t, err)
		assert.Equal(t, snapshot.KindPod, dest.Kind)
		assert.Equal(t, "my-baseline", dest.Value)
	})

	t.Run("rejects local path", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseShowable("./my-snapshot", cwd, home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "show local snapshots")
	})

	t.Run("rejects invalid pod name", func(t *testing.T) {
		t.Parallel()
		_, err := snapshot.ParseShowable("pod:bad name", cwd, home)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid pod name")
	})
}

func TestParseSource(t *testing.T) {
	t.Parallel()
	// Use a temp dir as home so the test doesn't depend on the real $HOME
	// (e.g. under Nix's sandboxed build, $HOME is a non-existent placeholder).
	home := t.TempDir()

	dir := t.TempDir()
	existingSnapshot := filepath.Join(dir, "snap.snapshot")
	require.NoError(t, os.WriteFile(existingSnapshot, []byte("data"), 0600))
	existingZip := filepath.Join(dir, "legacy.zip") // saved by an older lstk version
	require.NoError(t, os.WriteFile(existingZip, []byte("data"), 0600))
	existingBare := filepath.Join(dir, "bare") // no extension — .snapshot fallback exists
	require.NoError(t, os.WriteFile(existingBare+".snapshot", []byte("data"), 0600))
	existingLegacyBare := filepath.Join(dir, "legacybare") // only a .zip counterpart exists
	require.NoError(t, os.WriteFile(existingLegacyBare+".zip", []byte("data"), 0600))
	existingNoExt := filepath.Join(dir, "noext") // no extension, no fallback counterpart either
	require.NoError(t, os.WriteFile(existingNoExt, []byte("data"), 0600))
	plainDir := filepath.Join(dir, "some-directory")
	require.NoError(t, os.Mkdir(plainDir, 0o755))
	snapshotExtDir := filepath.Join(dir, "looks-like-a-snapshot.snapshot")
	require.NoError(t, os.Mkdir(snapshotExtDir, 0o755))

	type testCase struct {
		name          string
		input         string
		wantKind      snapshot.DestinationKind
		wantPath      string
		wantPodName   string
		wantErr       string
		wantRemoteErr bool
		wantSchemeErr bool
	}

	tests := []testCase{
		// --- empty ref ---
		{
			name:    "empty ref",
			input:   "",
			wantErr: "REF is required",
		},

		// --- local paths (file must exist) ---
		{
			name:     "explicit .snapshot path",
			input:    existingSnapshot,
			wantKind: snapshot.KindLocal,
			wantPath: existingSnapshot,
		},
		{
			name:     "explicit legacy .zip path",
			input:    existingZip,
			wantKind: snapshot.KindLocal,
			wantPath: existingZip,
		},
		{
			name:     "bare name resolves to .snapshot fallback",
			input:    existingBare,
			wantKind: snapshot.KindLocal,
			wantPath: existingBare + ".snapshot",
		},
		{
			name:     "bare name resolves to legacy .zip fallback",
			input:    existingLegacyBare,
			wantKind: snapshot.KindLocal,
			wantPath: existingLegacyBare + ".zip",
		},
		{
			name:     "file without extension resolves as-is",
			input:    existingNoExt,
			wantKind: snapshot.KindLocal,
			wantPath: existingNoExt,
		},
		{
			name:    "nonexistent file returns error",
			input:   filepath.Join(dir, "missing.snapshot"),
			wantErr: "snapshot file not found",
		},
		{
			name:    "nonexistent bare name returns error",
			input:   filepath.Join(dir, "ghost"),
			wantErr: "snapshot file not found",
		},
		{
			name:    "directory instead of file returns clear error",
			input:   plainDir,
			wantErr: "is a directory",
		},
		{
			name:    "directory with .snapshot extension returns clear error",
			input:   snapshotExtDir,
			wantErr: "is a directory",
		},
		{
			name:    "relative path resolved via cwd is a directory",
			input:   ".",
			wantErr: "is a directory",
		},

		// --- tilde expansion ---
		{
			name:    "tilde expands to home which is a directory",
			input:   "~/.",
			wantErr: "is a directory",
		},

		// --- pod sources ---
		{
			name:        "pod: prefix",
			input:       "pod:my-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "my-baseline",
		},
		{
			name:        "Pod: case insensitive",
			input:       "Pod:my-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "my-baseline",
		},
		{
			name:    "pod:// rejected with did-you-mean hint",
			input:   "pod://my-baseline",
			wantErr: "not a valid reference. Aliases use a single colon. Did you mean:\npod:my-baseline",
		},
		{
			name:    "pod: empty name",
			input:   "pod:",
			wantErr: "invalid pod name",
		},
		{
			name:        "pod: leading hyphen accepted (platform allows it)",
			input:       "pod:-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "-baseline",
		},
		{
			name:    "pod: percent encoding rejected",
			input:   "pod:staging%2Fpod",
			wantErr: "invalid pod name",
		},
		{
			name:    "pod: shell metacharacters rejected",
			input:   "pod:a;rm",
			wantErr: "invalid pod name",
		},

		// --- remote schemes ---
		{
			name:     "s3:// is an S3 remote",
			input:    "s3://bucket/key",
			wantKind: snapshot.KindS3,
			wantPath: "s3://bucket/key",
		},
		{
			name:    "s3:// rejects embedded credentials",
			input:   "s3://bucket/key?access_key_id=AKIA&secret_access_key=zzz",
			wantErr: "do not put credentials",
		},
		{
			name:    "s3:// requires a bucket",
			input:   "s3:///key",
			wantErr: "missing bucket",
		},
		{
			name:          "oras:// not supported",
			input:         "oras://registry/image",
			wantRemoteErr: true,
		},
		{
			name:          "unknown scheme",
			input:         "gcs://bucket/key",
			wantSchemeErr: true,
		},
	}

	if runtime.GOOS == "windows" {
		tests = append(tests,
			testCase{
				name:     "windows tilde backslash",
				input:    `~\` + filepath.Base(existingZip),
				wantKind: snapshot.KindLocal,
				// The resolved path won't equal existingZip (different dir), so just
				// check it doesn't error; path matching is covered by the cross-platform cases.
				wantErr: "snapshot file not found",
			},
			testCase{
				name:     "windows abs backslash to existing zip",
				input:    existingZip,
				wantKind: snapshot.KindLocal,
				wantPath: existingZip,
			},
			testCase{
				name:     "windows abs forward-slash to existing zip",
				input:    strings.ReplaceAll(existingZip, `\`, `/`),
				wantKind: snapshot.KindLocal,
				wantPath: existingZip,
			},
		)
	}

	for _, tc := range tests {
		name := tc.input
		if tc.name != "" {
			name = tc.name
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := snapshot.ParseSource(tc.input, home)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			if tc.wantRemoteErr {
				require.ErrorIs(t, err, snapshot.ErrRemoteNotSupported)
				return
			}
			if tc.wantSchemeErr {
				require.ErrorIs(t, err, snapshot.ErrUnknownScheme)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantKind, got.Kind)
			if tc.wantPodName != "" {
				assert.Equal(t, tc.wantPodName, got.Value)
			} else {
				assert.Equal(t, tc.wantPath, got.Value)
			}
		})
	}
}

// TestParseSourceTildeWithoutHome covers the Nix sandbox scenario where the
// build runs without a usable home directory. Tilde expansion must fail with a
// clear error instead of silently using a non-existent path.
func TestParseSourceTildeWithoutHome(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, input string }{
		{"bare tilde", "~"},
		{"tilde slash", "~/snap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := snapshot.ParseSource(tc.input, "")
			require.ErrorIs(t, err, snapshot.ErrHomeNotSet)
		})
	}
}

func TestDisplayPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	cwd := filepath.Join(base, "projects", "lstk")
	home := filepath.Join(base, "home")

	tests := []struct {
		name string
		abs  string
		cwd  string
		home string
		want string
	}{
		{
			name: "under cwd",
			abs:  filepath.Join(cwd, "snap.zip"),
			cwd:  cwd, home: home,
			want: "./snap.zip",
		},
		{
			name: "under cwd subdir",
			abs:  filepath.Join(cwd, "exports", "snap.zip"),
			cwd:  cwd, home: home,
			want: "./exports/snap.zip",
		},
		{
			name: "under home but not cwd",
			abs:  filepath.Join(home, "snap.zip"),
			cwd:  cwd, home: home,
			want: "~/snap.zip",
		},
		{
			name: "under home subdir",
			abs:  filepath.Join(home, "downloads", "snap.zip"),
			cwd:  cwd, home: home,
			want: "~/downloads/snap.zip",
		},
		{
			name: "unrelated to both",
			abs:  filepath.Join(base, "other", "snap.zip"),
			cwd:  cwd, home: home,
			want: filepath.Join(base, "other", "snap.zip"),
		},
		{
			name: "empty cwd falls back to home",
			abs:  filepath.Join(home, "snap.zip"),
			cwd:  "", home: home,
			want: "~/snap.zip",
		},
		{
			name: "empty cwd and home returns absolute",
			abs:  filepath.Join(base, "snap.zip"),
			cwd:  "", home: "",
			want: filepath.Join(base, "snap.zip"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, snapshot.DisplayPath(tc.abs, tc.cwd, tc.home))
		})
	}
}

func TestParseDestination(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	// Use a temp dir as home so the test doesn't depend on the real $HOME
	// (e.g. under Nix's sandboxed build, $HOME is a non-existent placeholder).
	home := t.TempDir()

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)

	// Set up dirs used in path-based cases below.
	existingDir := t.TempDir()
	subDir := filepath.Join(existingDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))

	type testCase struct {
		name           string // optional; uses input when empty
		input          string
		wantKind       snapshot.DestinationKind
		wantPath       string
		wantPodName    string
		wantPathRegexp string // used instead of wantPath when the result contains a random component
		wantErr        string
		wantRemoteErr  bool
		wantSchemeErr  bool
	}

	tests := []testCase{
		// --- default (empty input) ---
		{
			name:           "default",
			input:          "",
			wantKind:       snapshot.KindLocal,
			wantPathRegexp: regexp.QuoteMeta(filepath.Join(wd, "snapshot-2026-05-11T21-04-32-")) + `[0-9a-f]{3}\.snapshot`,
		},

		// --- local paths ---
		{
			input:    "./my-state",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "my-state.snapshot"),
		},
		{
			input:    filepath.Join(os.TempDir(), "state"),
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(os.TempDir(), "state.snapshot"),
		},
		{
			input:   "~",
			wantErr: "is a directory",
		},
		{
			// parent (~/) always exists
			input:    "~/my-state",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(home, "my-state.snapshot"),
		},
		{
			name:     "relative path with existing subdir",
			input:    filepath.Join(subDir, "state"),
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(subDir, "state.snapshot"),
		},
		{
			// bare name: treated as relative to CWD, not a pod
			input:    "my-pod",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "my-pod.snapshot"),
		},
		{
			name:     "explicit .snapshot extension kept",
			input:    "./checkpoint.snapshot",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "checkpoint.snapshot"),
		},
		{
			name:     "uppercase .SNAPSHOT extension kept as-is",
			input:    "./already.SNAPSHOT",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "already.SNAPSHOT"),
		},
		{
			name:     "explicit .zip extension forced to .snapshot",
			input:    "./checkpoint.zip",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "checkpoint.snapshot"),
		},
		{
			name:     "other extension forced to .snapshot",
			input:    "./backup.tar",
			wantKind: snapshot.KindLocal,
			wantPath: filepath.Join(wd, "backup.snapshot"),
		},

		// --- parent directory does not exist ---
		{
			name:    "parent dir missing",
			input:   filepath.Join(existingDir, "nonexistent", "state"),
			wantErr: "parent directory",
		},

		// --- remote: s3 ---
		{
			input:    "s3://bucket/key",
			wantKind: snapshot.KindS3,
			wantPath: "s3://bucket/key",
		},
		{
			input:    "S3://bucket/key",
			wantKind: snapshot.KindS3,
			wantPath: "S3://bucket/key",
		},
		{
			name:    "s3:// rejects embedded credentials",
			input:   "s3://bucket/key?secret_access_key=zzz",
			wantErr: "do not put credentials",
		},

		// --- remote: oras ---
		{
			input:         "oras://registry/image",
			wantRemoteErr: true,
		},
		{
			input:         "ORAS://registry/image",
			wantRemoteErr: true,
		},

		// --- pod destinations ---
		{
			input:       "pod:my-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "my-baseline",
		},
		{
			input:       "Pod:my-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "my-baseline",
		},
		{
			name:    "pod:// rejected with did-you-mean hint",
			input:   "pod://my-baseline",
			wantErr: "not a valid reference. Aliases use a single colon. Did you mean:\npod:my-baseline",
		},
		{
			input:       "pod:abc123",
			wantKind:    snapshot.KindPod,
			wantPodName: "abc123",
		},
		{
			input:       "pod:my-long-pod-name-123",
			wantKind:    snapshot.KindPod,
			wantPodName: "my-long-pod-name-123",
		},
		{
			// empty pod name
			name:    "pod: empty name",
			input:   "pod:",
			wantErr: "invalid pod name",
		},
		{
			// leading hyphen is inside the platform's POD_NAME_PATTERN
			name:        "pod: leading hyphen accepted (platform allows it)",
			input:       "pod:-baseline",
			wantKind:    snapshot.KindPod,
			wantPodName: "-baseline",
		},
		{
			name:        "pod: underscore allowed",
			input:       "pod:ci_test-underscore",
			wantKind:    snapshot.KindPod,
			wantPodName: "ci_test-underscore",
		},
		{
			name:    "pod: percent encoding rejected",
			input:   "pod:staging%2Fpod",
			wantErr: "invalid pod name",
		},
		{
			name:    "pod: embedded query rejected",
			input:   "pod:abc?fields=name",
			wantErr: "invalid pod name",
		},
		{
			name:    "pod: shell metacharacters rejected",
			input:   "pod:a;rm",
			wantErr: "invalid pod name",
		},

		// --- unknown schemes ---
		{
			input:         "https://example.com/snap",
			wantSchemeErr: true,
		},
		{
			input:         "gcs://bucket/key",
			wantSchemeErr: true,
		},
	}

	if runtime.GOOS == "windows" {
		tmpParent := filepath.Clean(os.TempDir())
		tests = append(tests,
			testCase{
				input:    `~\my-state`,
				wantKind: snapshot.KindLocal,
				wantPath: filepath.Join(home, "my-state.snapshot"),
			},
			testCase{
				name:     "windows abs backslash",
				input:    filepath.Join(tmpParent, "snap"),
				wantKind: snapshot.KindLocal,
				wantPath: filepath.Join(tmpParent, "snap.snapshot"),
			},
			testCase{
				name:     "windows abs forward-slash",
				input:    strings.ReplaceAll(filepath.Join(tmpParent, "snap"), `\`, `/`),
				wantKind: snapshot.KindLocal,
				wantPath: filepath.Join(tmpParent, "snap.snapshot"),
			},
		)
	}

	for _, tc := range tests {
		name := tc.input
		if tc.name != "" {
			name = tc.name
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := snapshot.ParseDestination(tc.input, home, now)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			if tc.wantRemoteErr {
				require.ErrorIs(t, err, snapshot.ErrRemoteNotSupported)
				return
			}
			if tc.wantSchemeErr {
				require.ErrorIs(t, err, snapshot.ErrUnknownScheme)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantKind, got.Kind)
			if tc.wantPodName != "" {
				assert.Equal(t, tc.wantPodName, got.Value)
			} else if tc.wantPathRegexp != "" {
				assert.Regexp(t, tc.wantPathRegexp, got.Value)
			} else {
				assert.Equal(t, tc.wantPath, got.Value)
			}
		})
	}
}

// TestParseDestinationTildeWithoutHome covers the Nix sandbox scenario where
// the build runs without a usable home directory. Tilde expansion must fail
// with a clear error instead of silently using a non-existent path.
func TestParseDestinationTildeWithoutHome(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)

	tests := []struct{ name, input string }{
		{"bare tilde", "~"},
		{"tilde slash", "~/snap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := snapshot.ParseDestination(tc.input, "", now)
			require.ErrorIs(t, err, snapshot.ErrHomeNotSet)
		})
	}
}

func TestDefaultRemotePodName(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)
	name := snapshot.DefaultRemotePodName(now)

	assert.True(t, strings.HasPrefix(name, "snapshot-2026-05-11T21-04-32-"), "got %q", name)
	// The generated name must be a valid pod name.
	require.NoError(t, snapshot.ValidatePodName(name))
	// The random suffix should make repeated calls distinct.
	assert.NotEqual(t, name, snapshot.DefaultRemotePodName(now))
}
