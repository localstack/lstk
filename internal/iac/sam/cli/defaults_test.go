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
