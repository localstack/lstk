package eksctl

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckVersionString(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		wantErr bool
	}{
		{"exact minimum", "0.181.0", false},
		{"newer patch", "0.181.5", false},
		{"newer minor", "0.211.0", false},
		{"much newer", "1.2.3", false},
		{"too old patch", "0.180.9", true},
		{"too old minor", "0.167.0", true},
		{"unparseable", "not a version", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkVersionString(tt.out)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCheckVersionStringMessageMentionsMinimum(t *testing.T) {
	err := checkVersionString("0.180.0")
	assert.ErrorContains(t, err, minEksctlVersionString)
}

func TestCheckVersionFailsClosedWhenVersionCommandFails(t *testing.T) {
	// A binary that cannot report its version must be rejected, not run.
	err := CheckVersion(context.Background(), filepath.Join(t.TempDir(), "missing-eksctl"))
	assert.ErrorContains(t, err, "could not determine eksctl version")
}
