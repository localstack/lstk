package integration_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogoutCommandRemovesToken(t *testing.T) {
	// Clean up any existing token
	_ = DeleteAuthTokenFromKeyring()
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	// Store a token in keyring
	err := SetAuthTokenInKeyring("test-token")
	require.NoError(t, err, "failed to store token in keyring")

	// Run logout command
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "logout")
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk logout failed: %s", output)
	assert.Contains(t, string(output), "Logged out successfully")

	// Verify token was removed
	_, err = GetAuthTokenFromKeyring()
	assert.Error(t, err, "token should be removed from keyring")
}

func TestLogoutCommandSucceedsWhenNoToken(t *testing.T) {
	// Ensure no token exists
	_ = DeleteAuthTokenFromKeyring()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "logout")
	output, err := cmd.CombinedOutput()

	// Should succeed even if no token
	require.NoError(t, err, "lstk logout should succeed even with no token: %s", output)
}
