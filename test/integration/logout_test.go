package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types/image"
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
	assert.Contains(t, stdout, "LocalStack AWS Emulator is still running in the background")
}

func TestLogoutCommandReportsBothEmulatorsWhenMultipleRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	ctx := testContext(t)

	const fakeAWSImage = "localstack/localstack-pro:test-fake"
	const fakeSnowflakeImage = "localstack/snowflake:test-fake"
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeAWSImage))
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeSnowflakeImage))
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeAWSImage, image.RemoveOptions{})
		_, _ = dockerClient.ImageRemove(context.Background(), fakeSnowflakeImage, image.RemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeAWSImage, "localstack-external-aws", "4566")
	startExternalContainer(t, ctx, fakeSnowflakeImage, "localstack-external-snowflake", "4567")

	require.NoError(t, SetAuthTokenInKeyring("test-token"), "failed to store token in keyring")

	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type = "aws"
tag  = "test-fake"
port = "4566"

[[containers]]
type = "snowflake"
tag  = "test-fake"
port = "4567"
`), 0644))

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "--config", configFile, "logout")
	require.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "LocalStack AWS Emulator, LocalStack Snowflake Emulator are still running in the background")
}

func TestLogoutCommandDoesNotReportForeignEmulatorAsRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	ctx := testContext(t)

	// AWS image running on 4566 while config targets snowflake.
	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeImage, "localstack-external-aws", "4566")

	require.NoError(t, SetAuthTokenInKeyring("test-token"), "failed to store token in keyring")

	configFile := writeSnowflakeConfig(t, "4566")

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "--config", configFile, "logout")
	require.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.NotContains(t, stdout, "still running",
		"snowflake-targeted logout should not detect the AWS container as the configured emulator")
}
