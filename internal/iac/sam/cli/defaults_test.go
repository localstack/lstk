package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsOffline(t *testing.T) {
	offline := [][]string{
		{"docs"},
		{"init", "--name", "demo"},
		{"build"},
		{"validate", "--lint"},
		{"local", "generate-event", "s3", "put"},
		{"local", "invoke"},
		{"pipeline", "init"},
		{"--help"},
		{"-h"},
		{"deploy", "--help"},
	}
	for _, args := range offline {
		assert.Truef(t, IsOffline(args), "expected %v offline", args)
	}

	awsContacting := [][]string{
		{"deploy", "--stack-name", "demo"},
		{"sync"},
		{"package"},
		{"delete"},
		{"logs"},
		{"traces"},
		{"list", "resources", "--stack-name", "demo"},
		{"remote", "invoke"},
		{"publish"},
		{}, // no subcommand → not offline (gate on emulator)
	}
	for _, args := range awsContacting {
		assert.Falsef(t, IsOffline(args), "expected %v not offline", args)
	}
}

// The classifier keys on the first non-flag (top-level) token; leading flags are
// skipped.
func TestSubcommandSkipsLeadingFlags(t *testing.T) {
	assert.Equal(t, "deploy", subcommand([]string{"--debug", "deploy", "--stack-name", "demo"}))
	assert.Equal(t, "build", subcommand([]string{"--beta-features", "build"}))
	assert.Equal(t, "", subcommand([]string{"--debug"}))
	assert.Equal(t, "", subcommand(nil))
}

// Two-level commands resolve to their top-level token: `local generate-event` is
// offline (under `local`), `list resources` is AWS-contacting (under `list`).
func TestTwoLevelCommandsKeyOnTopLevelToken(t *testing.T) {
	assert.True(t, IsOffline([]string{"local", "generate-event"}))
	assert.False(t, IsOffline([]string{"list", "resources"}))
}

// The region must reach SAM as a command-line option, not only as AWS_REGION:
// samconfig.toml values are injected by SAM as if typed on the command line, so
// a `region` key there outranks every environment variable.
func TestWithRegionFlag(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		region string
		want   []string
	}{
		{
			name:   "appended for an AWS-contacting subcommand",
			args:   []string{"deploy", "--no-confirm-changeset"},
			region: "ap-northeast-1",
			want:   []string{"deploy", "--no-confirm-changeset", "--region", "ap-northeast-1"},
		},
		{
			name:   "appended for a nested subcommand",
			args:   []string{"list", "resources", "--stack-name", "demo"},
			region: "eu-west-3",
			want:   []string{"list", "resources", "--stack-name", "demo", "--region", "eu-west-3"},
		},
		{
			// `init` and `docs` reject --region outright; both are offline.
			name:   "not appended for an offline subcommand",
			args:   []string{"init", "--name", "demo"},
			region: "ap-northeast-1",
			want:   []string{"init", "--name", "demo"},
		},
		{
			name:   "not appended for a help request",
			args:   []string{"deploy", "--help"},
			region: "ap-northeast-1",
			want:   []string{"deploy", "--help"},
		},
		{
			// The user is addressing sam directly; appending ours after theirs
			// would silently outrank it.
			name:   "user's own region flag is left alone",
			args:   []string{"deploy", "--region", "us-west-1"},
			region: "ap-northeast-1",
			want:   []string{"deploy", "--region", "us-west-1"},
		},
		{
			name:   "user's own region equals form is left alone",
			args:   []string{"deploy", "--region=us-west-1"},
			region: "ap-northeast-1",
			want:   []string{"deploy", "--region=us-west-1"},
		},
		{
			name:   "empty region is a no-op",
			args:   []string{"deploy"},
			region: "",
			want:   []string{"deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, withRegionFlag(tt.args, tt.region))
		})
	}
}

func TestWithRegionFlagDoesNotMutateInput(t *testing.T) {
	args := []string{"deploy", "--no-confirm-changeset"}
	original := make([]string, len(args))
	copy(original, args)

	withRegionFlag(args, "ap-northeast-1")

	assert.Equal(t, original, args)
}
