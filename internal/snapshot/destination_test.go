package snapshot_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDestinationDefault(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)
	got, err := snapshot.ParseDestination("", now)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "snapshot-2026-05-11T21-04-32"), got)
}

func TestParseDestination(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)

	type testCase struct {
		input    string
		wantPath string
		wantErr  string
	}

	tests := []testCase{
		{
			input:    "./my-state",
			wantPath: filepath.Join(wd, "my-state"),
		},
		{
			input:    filepath.Join(os.TempDir(), "state"),
			wantPath: filepath.Join(os.TempDir(), "state"),
		},
		{
			input:    "~",
			wantPath: home,
		},
		{
			input:    "~/snapshots/s",
			wantPath: filepath.Join(home, "snapshots", "s"),
		},
		{
			input:    "subdir/state",
			wantPath: filepath.Join(wd, "subdir", "state"),
		},
		{
			input:   "my-pod",
			wantErr: "cloud destinations are not yet supported",
		},
		{
			input:   "cloud://my-pod",
			wantErr: "cloud destinations are not yet supported",
		},
		{
			input:   "s3://bucket/key",
			wantErr: "cloud destinations are not yet supported",
		},
	}

	if runtime.GOOS == "windows" {
		tests = append(tests,
			testCase{input: `~\snapshots\s`, wantPath: filepath.Join(home, "snapshots", "s")},
			testCase{input: `C:\Users\user\snap`, wantPath: `C:\Users\user\snap`},
			testCase{input: `C:/Users/user/snap`, wantPath: `C:\Users\user\snap`},
		)
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := snapshot.ParseDestination(tc.input, now)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), "./my-snapshot")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPath, got)
		})
	}
}
