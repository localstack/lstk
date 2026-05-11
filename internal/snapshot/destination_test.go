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

func TestParseDestinationRejectsDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)
	_, err := snapshot.ParseDestination(dir, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestParseDestinationDefault(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)
	got, err := snapshot.ParseDestination("", now)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "snapshot-2026-05-11T21-04-32.zip"), got)
}

func TestParseDestination(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	now := time.Date(2026, 5, 11, 21, 4, 32, 0, time.UTC)

	type testCase struct {
		input        string
		wantPath     string
		wantErr      string
		wantCloudErr bool
	}

	tests := []testCase{
		{
			input:    "./my-state",
			wantPath: filepath.Join(wd, "my-state.zip"),
		},
		{
			input:    filepath.Join(os.TempDir(), "state"),
			wantPath: filepath.Join(os.TempDir(), "state.zip"),
		},
		{
			input:    "~",
			wantErr:  "is a directory",
		},
		{
			input:    "~/snapshots/s",
			wantPath: filepath.Join(home, "snapshots", "s.zip"),
		},
		{
			input:    "subdir/state",
			wantPath: filepath.Join(wd, "subdir", "state.zip"),
		},
		{
			input:    "./checkpoint.zip",
			wantPath: filepath.Join(wd, "checkpoint.zip"),
		},
		{
			input:    "./already.ZIP",
			wantPath: filepath.Join(wd, "already.ZIP"),
		},
		{
			input:        "my-pod",
			wantErr:      "cloud destinations are not yet supported",
			wantCloudErr: true,
		},
		{
			input:        "cloud://my-pod",
			wantErr:      "cloud destinations are not yet supported",
			wantCloudErr: true,
		},
		{
			input:        "s3://bucket/key",
			wantErr:      "cloud destinations are not yet supported",
			wantCloudErr: true,
		},
	}

	if runtime.GOOS == "windows" {
		tests = append(tests,
			testCase{input: `~\snapshots\s`, wantPath: filepath.Join(home, "snapshots", "s.zip")},
			testCase{input: `C:\Users\user\snap`, wantPath: `C:\Users\user\snap.zip`},
			testCase{input: `C:/Users/user/snap`, wantPath: `C:\Users\user\snap.zip`},
		)
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := snapshot.ParseDestination(tc.input, now)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				if tc.wantCloudErr {
					assert.Contains(t, err.Error(), "./my-snapshot.zip")
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantPath, got)
		})
	}
}
