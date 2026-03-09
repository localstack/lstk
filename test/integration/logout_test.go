package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogoutCommandRemovesToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	err := SetAuthTokenInKeyring("test-token")
	require.NoError(t, err, "failed to store token in keyring")

	stdout, stderr, err := runLstk(t, testContext(t), "", nil, "logout")
	require.NoError(t, err, "lstk logout failed: %s", stderr)
	assert.Contains(t, stdout, "Logged out successfully")

	_, err = GetAuthTokenFromKeyring()
	assert.Error(t, err, "token should be removed from keyring")
}

func TestLogoutCommandSucceedsWhenNoToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken), "logout")
	require.NoError(t, err, "lstk logout should succeed even with no token: %s", stderr)
	assert.Contains(t, stdout, "Not currently logged in")
}

func TestLogoutCommandWithEnvVarToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.AuthToken, "test-env-token"), "logout")
	require.NoError(t, err, "lstk logout should succeed: %s", stderr)
	assert.Contains(t, stdout, "LOCALSTACK_AUTH_TOKEN")
}
