package snapshot_test

import (
	"testing"

	"github.com/localstack/lstk/internal/snapshot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDestination(t *testing.T) {
	tests := []struct {
		input   string
		want    snapshot.Destination
		wantErr string
	}{
		{
			input: "",
			want:  snapshot.LocalDestination{Path: "ls-state-export"},
		},
		{
			input: "./my-state",
			want:  snapshot.LocalDestination{Path: "./my-state"},
		},
		{
			input: "/tmp/state",
			want:  snapshot.LocalDestination{Path: "/tmp/state"},
		},
		{
			input: "~/snapshots/s",
			want:  snapshot.LocalDestination{Path: "~/snapshots/s"},
		},
		{
			input: "subdir/state",
			want:  snapshot.LocalDestination{Path: "subdir/state"},
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
			assert.Equal(t, tc.want, got)
		})
	}
}
