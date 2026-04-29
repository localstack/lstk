package snapshot_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDestination(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		input    string
		wantPath string
		wantErr  string
	}{
		{
			input:    "",
			wantPath: filepath.Join(wd, "ls-state-export"),
		},
		{
			input:    "./my-state",
			wantPath: filepath.Join(wd, "my-state"),
		},
		{
			input:    filepath.Join(os.TempDir(), "state"),
			wantPath: filepath.Join(os.TempDir(), "state"),
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

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := snapshot.ParseDestination(tc.input)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Contains(t, err.Error(), "./my-snapshot")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, snapshot.LocalDestination{Path: tc.wantPath}, got)
		})
	}
}
