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
	wd, err := os.Getwd()
	require.NoError(t, err)
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
			name:     "relative path resolved via cwd",
			input:    ".",
			wantKind: snapshot.KindLocal,
			wantPath: wd,
		},

		// --- tilde expansion ---
		{
			name:     "tilde expands to home",
			input:    "~/.",
			wantKind: snapshot.KindLocal,
			wantPath: home,
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
			name:    "pod: leading hyphen",
			input:   "pod:-bad",
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
			// pod name starting with hyphen
			name:    "pod: leading hyphen",
			input:   "pod:-invalid",
			wantErr: "invalid pod name",
		},
		{
			name:        "pod: underscore allowed",
			input:       "pod:ci_test-underscore",
			wantKind:    snapshot.KindPod,
			wantPodName: "ci_test-underscore",
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
