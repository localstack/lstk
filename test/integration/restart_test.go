package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestartCommandSucceeds(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).With(env.AnalyticsEndpoint, analyticsSrv.URL), "restart")
	require.NoError(t, err, "lstk restart failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "stopped")
	assert.Contains(t, stdout, "LocalStack")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container after restart")
	assert.True(t, inspect.State.Running, "container should be running after restart")

	// Both lstk_lifecycle (stop + start) and lstk_command events should be emitted.
	byName := collectTelemetryByName(t, events, 2)
	assert.Contains(t, byName, "lstk_lifecycle")
	if cmdEvent, ok := byName["lstk_command"]; assert.True(t, ok, "lstk_command event not received") {
		payload, _ := cmdEvent["payload"].(map[string]any)
		params, _ := payload["parameters"].(map[string]any)
		assert.Equal(t, "restart", params["command"])
		result, _ := payload["result"].(map[string]any)
		assert.InDelta(t, 0, result["exit_code"], 0)
	}
}

func TestRestartCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	analyticsSrv, events := mockAnalyticsServer(t)
	_, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "restart")
	require.Error(t, err, "expected lstk restart to fail when emulator is not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "is not running")
	assertCommandTelemetry(t, events, "restart", 1)
}
