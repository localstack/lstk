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
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)

	// Set up dirs used in path-based cases below.
	existingDir := t.TempDir()
	subDir := filepath.Join(existingDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o755))

	type testCase struct {
		name           string // optional; uses input when empty
		input          string
		wantPath       string
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
			wantPathRegexp: regexp.QuoteMeta(filepath.Join(wd, "snapshot-2026-05-11T21-04-32-")) + `[0-9a-f]{3}\.zip`,
		},

		// --- local paths ---
		{
			input:    "./my-state",
			wantPath: filepath.Join(wd, "my-state.zip"),
		},
		{
			input:    filepath.Join(os.TempDir(), "state"),
			wantPath: filepath.Join(os.TempDir(), "state.zip"),
		},
		{
			input:   "~",
			wantErr: "is a directory",
		},
		{
			// parent (~/) always exists
			input:    "~/my-state",
			wantPath: filepath.Join(home, "my-state.zip"),
		},
		{
			name:     "relative path with existing subdir",
			input:    filepath.Join(subDir, "state"),
			wantPath: filepath.Join(subDir, "state.zip"),
		},
		{
			// bare name: treated as relative to CWD
			input:    "my-pod",
			wantPath: filepath.Join(wd, "my-pod.zip"),
		},
		{
			input:    "./checkpoint.zip",
			wantPath: filepath.Join(wd, "checkpoint.zip"),
		},
		{
			input:    "./already.ZIP",
			wantPath: filepath.Join(wd, "already.ZIP"),
		},

		// --- parent directory does not exist ---
		{
			name:    "parent dir missing",
			input:   filepath.Join(existingDir, "nonexistent", "state"),
			wantErr: "parent directory",
		},

		// --- remote: s3 ---
		{
			input:         "s3://bucket/key",
			wantRemoteErr: true,
		},
		{
			input:         "S3://bucket/key",
			wantRemoteErr: true,
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

		// --- remote: cloud ---
		{
			input:         "cloud:my-pod",
			wantRemoteErr: true,
		},
		{
			input:         "Cloud:my-pod",
			wantRemoteErr: true,
		},
		{
			// cloud: prefix also catches cloud://
			input:         "cloud://my-pod",
			wantRemoteErr: true,
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
				wantPath: filepath.Join(home, "my-state.zip"),
			},
			testCase{
				name:     "windows abs backslash",
				input:    filepath.Join(tmpParent, "snap"),
				wantPath: filepath.Join(tmpParent, "snap.zip"),
			},
			testCase{
				name:     "windows abs forward-slash",
				input:    strings.ReplaceAll(filepath.Join(tmpParent, "snap"), `\`, `/`),
				wantPath: filepath.Join(tmpParent, "snap.zip"),
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
			got, err := snapshot.ParseDestination(tc.input, now)
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
			if tc.wantPathRegexp != "" {
				assert.Regexp(t, tc.wantPathRegexp, got)
			} else {
				assert.Equal(t, tc.wantPath, got)
			}
		})
	}
}
