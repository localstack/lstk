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

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "logout")
	require.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Logged out successfully")

	_, err = GetAuthTokenFromKeyring()
	assert.Error(t, err, "token should be removed from keyring")
	assertCommandTelemetry(t, events, "logout", 0)
}

func TestLogoutCommandSucceedsWhenNoToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.AnalyticsEndpoint, analyticsSrv.URL), "logout")
	require.NoError(t, err, "lstk logout should succeed even with no token: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Not currently logged in")
	assertCommandTelemetry(t, events, "logout", 0)
}

func TestLogoutCommandWithEnvVarToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.AuthToken, "test-env-token"), "logout")
	require.NoError(t, err, "lstk logout should succeed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "LOCALSTACK_AUTH_TOKEN")
}

func TestLogoutCommandNotesWhenEmulatorStillRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	ctx := testContext(t)
	startTestContainer(t, ctx)

	err := SetAuthTokenInKeyring("test-token")
	require.NoError(t, err, "failed to store token in keyring")

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "logout")
	require.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "LocalStack is still running in the background")
}
