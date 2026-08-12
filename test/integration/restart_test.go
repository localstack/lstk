package integration_test

import (
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
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
	must.NoError(t, err, "lstk start failed: %s", stderr)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).With(env.AnalyticsEndpoint, analyticsSrv.URL), "restart")
	must.NoError(t, err, "lstk restart failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "stopped")
	must.Contains(t, stdout, "LocalStack")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.NoError(t, err, "failed to inspect container after restart")
	must.True(t, inspect.Container.State.Running, "container should be running after restart")

	// Both lstk_lifecycle (stop + start) and lstk_command events should be emitted.
	byName := collectTelemetryByName(t, events, 2)
	must.Contains(t, byName, "lstk_lifecycle")
	cmdEvent, ok := byName["lstk_command"]
	must.True(t, ok, "lstk_command event not received")
	{
		payload, _ := cmdEvent["payload"].(map[string]any)
		params, _ := payload["parameters"].(map[string]any)
		must.Eq(t, "restart", params["command"])
		result, _ := payload["result"].(map[string]any)
		must.InDelta(t, 0, result["exit_code"], 0)
	}
}

// TestRestartCommandRejectsAmbientEndpointURLEvenWithLocalContainerRunning is
// the exact bug scenario reported: `lstk restart` with LSTK_ENDPOINT_URL
// ambiently set used to silently proceed against a running local container
// and, when none was running, surfaced a confusing generic "not running"
// error unrelated to the env var. It must now reject outright — and, per
// design.md's Decision 5, that rejection must not depend on whether a local
// emulator happens to be running: it fires here even though one genuinely
// is.
func TestRestartCommandRejectsAmbientEndpointURLEvenWithLocalContainerRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, _, err := runLstk(t, ctx, "", env.With(env.DisableEvents, "1").With("LSTK_ENDPOINT_URL", "http://127.0.0.1:1"), "restart")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
	must.Contains(t, stdout, "LSTK_ENDPOINT_URL is set")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.NoError(t, err, "container should still exist — restart must reject before stopping/restarting it")
	must.True(t, inspect.Container.State.Running, "container should still be running, untouched")
}

func TestRestartCommandPersistFlagSetsPersistenceEnv(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	must.NoError(t, err, "lstk start failed: %s", stderr)

	_, stderr, err = runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "restart", "--persist")
	must.NoError(t, err, "lstk restart --persist failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.NoError(t, err, "failed to inspect container after restart")
	must.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	must.Eq(t, "1", envVars["LOCALSTACK_PERSISTENCE"])
}

func TestRestartCommandPreservesPersistenceWithoutFlag(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start", "--persist")
	must.NoError(t, err, "lstk start --persist failed: %s", stderr)

	// Restart without --persist should carry the setting forward from the running instance.
	_, stderr, err = runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "restart")
	must.NoError(t, err, "lstk restart failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.NoError(t, err, "failed to inspect container after restart")
	must.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	must.Eq(t, "1", envVars["LOCALSTACK_PERSISTENCE"])
}

func TestRestartCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "restart")
	must.Error(t, err, "expected lstk restart to fail when emulator is not running")
	requireExitCode(t, 1, err)
	must.Contains(t, stdout, "LocalStack AWS Emulator is not running")
	assertCommandTelemetry(t, events, "restart", 1)
}
