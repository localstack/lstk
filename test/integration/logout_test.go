package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
)

func TestLogoutCommandRemovesToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()
	t.Cleanup(func() {
		_ = DeleteAuthTokenFromKeyring()
	})

	err := SetAuthTokenInKeyring("test-token")
	must.NoError(t, err, "failed to store token in keyring")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "logout")
	must.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "Logged out successfully")

	_, err = GetAuthTokenFromKeyring()
	must.Error(t, err, "token should be removed from keyring")
	assertCommandTelemetry(t, events, "logout", 0)
}

func TestLogoutCommandSucceedsWhenNoToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.AnalyticsEndpoint, analyticsSrv.URL), "logout")
	must.NoError(t, err, "lstk logout should succeed even with no token: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "Not currently logged in")
	assertCommandTelemetry(t, events, "logout", 0)
}

func TestLogoutCommandWithEnvVarToken(t *testing.T) {
	_ = DeleteAuthTokenFromKeyring()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.Without(env.AuthToken).With(env.AuthToken, "test-env-token"), "logout")
	must.NoError(t, err, "lstk logout should succeed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "LOCALSTACK_AUTH_TOKEN")
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
	must.NoError(t, err, "failed to store token in keyring")

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "logout")
	must.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "LocalStack AWS Emulator is still running in the background")
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
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeAWSImage})
	must.NoError(t, err)
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeSnowflakeImage})
	must.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeAWSImage, client.ImageRemoveOptions{})
		_, _ = dockerClient.ImageRemove(context.Background(), fakeSnowflakeImage, client.ImageRemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeAWSImage, "localstack-external-aws", "4566")
	startExternalContainer(t, ctx, fakeSnowflakeImage, "localstack-external-snowflake", "4567")

	must.NoError(t, SetAuthTokenInKeyring("test-token"), "failed to store token in keyring")

	configFile := filepath.Join(t.TempDir(), "config.toml")
	must.NoError(t, os.WriteFile(configFile, []byte(`
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
	must.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "LocalStack AWS Emulator, LocalStack Snowflake Emulator are still running in the background")
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
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	must.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeImage, "localstack-external-aws", "4566")

	must.NoError(t, SetAuthTokenInKeyring("test-token"), "failed to store token in keyring")

	configFile := writeSnowflakeConfig(t, "4566")

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "--config", configFile, "logout")
	must.NoError(t, err, "lstk logout failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.NotContains(t, stdout, "still running",
		"snowflake-targeted logout should not detect the AWS container as the configured emulator")
}
