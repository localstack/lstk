package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
)

func TestStopCommandSucceeds(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "stop")
	must.NoError(t, err, "lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "Stopping", "should show stopping message")
	must.Contains(t, stdout, "stopped", "should show stopped message")

	_, err = dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.Error(t, err, "container should not exist after stop")

	// Both lstk_lifecycle (stop) and lstk_command events should be emitted.
	byName := collectTelemetryByName(t, events, 2)
	must.Contains(t, byName, "lstk_lifecycle")
	must.Contains(t, byName, "lstk_command")
}

// TestStopCommandRejectsAmbientEndpointURLEvenWithLocalContainerRunning
// proves the corrected behavior (design.md's Decision 5 for
// add-endpoint-url-flag): an ambient LSTK_ENDPOINT_URL rejects `stop`
// unconditionally, even though a local Docker-managed emulator is genuinely
// running and reachable — the original (buggy) behavior silently proceeded
// to stop this exact container instead.
func TestStopCommandRejectsAmbientEndpointURLEvenWithLocalContainerRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, _, err := runLstk(t, ctx, "", env.With(env.DisableEvents, "1").With("LSTK_ENDPOINT_URL", "http://127.0.0.1:1"), "stop")
	must.Error(t, err)
	must.Contains(t, stdout, "does not support LSTK_ENDPOINT_URL")
	must.Contains(t, stdout, "LSTK_ENDPOINT_URL is set")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.NoError(t, err, "container should still exist — stop must reject before ever touching it")
	must.True(t, inspect.Container.State.Running, "container should still be running")
}

func TestStopCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "stop")
	must.Error(t, err, "expected lstk stop to fail when container not running")
	requireExitCode(t, 1, err)
	must.Contains(t, stdout, "LocalStack AWS Emulator is not running")
	assertCommandTelemetry(t, events, "stop", 1)
}

func TestStopCommandReportsEmulatorSpecificNotRunningMessage(t *testing.T) {
	requireDocker(t)
	cleanupSnowflake()
	t.Cleanup(cleanupSnowflake)

	configFile := writeSnowflakeConfig(t, "4566")

	analyticsSrv, events := mockAnalyticsServer(t)
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AnalyticsEndpoint, analyticsSrv.URL)
	stdout, _, err := runLstk(t, testContext(t), "", e, "--config", configFile, "stop")
	must.Error(t, err, "expected lstk stop to fail when snowflake container not running")
	requireExitCode(t, 1, err)
	must.Contains(t, stdout, "LocalStack Snowflake Emulator is not running",
		"stop should match status's emulator-specific message")
	assertCommandTelemetry(t, events, "stop", 1)
}

func TestStopCommandIgnoresForeignEmulatorOnPort(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)

	// AWS image running on 4566 while config targets snowflake.
	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	must.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeImage, "localstack-external-aws", "4566")

	configFile := writeSnowflakeConfig(t, "4566")

	stdout, _, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "--config", configFile, "stop")
	must.Error(t, err, "lstk stop should not match foreign emulator on configured port")
	requireExitCode(t, 1, err)
	must.Contains(t, stdout, "LocalStack Snowflake Emulator is not running")
	must.NotContains(t, stdout, "stopped", "should not have stopped the AWS container")

	_, inspectErr := dockerClient.ContainerInspect(ctx, "localstack-external-aws", client.ContainerInspectOptions{})
	must.NoError(t, inspectErr, "AWS container should still exist after snowflake-targeted stop")
}

func TestStopCommandStopsExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	must.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-external", "4566")

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "stop")
	must.NoError(t, err, "lstk stop should stop external container: %s", stderr)
	requireExitCode(t, 0, err)
	must.Contains(t, stdout, "stopped")

	_, err = dockerClient.ContainerInspect(ctx, "localstack-external", client.ContainerInspectOptions{})
	must.Error(t, err, "external container should be gone after lstk stop")
}

func TestStopCommandIsIdempotent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	e := testEnvWithHome(t.TempDir(), "")
	_, stderr, err := runLstk(t, ctx, "", e, "stop")
	must.NoError(t, err, "first lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)

	_, err = dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	must.Error(t, err, "container should not exist after first stop")

	_, _, err = runLstk(t, ctx, "", e, "stop")
	must.Error(t, err, "second lstk stop should fail since container already removed")
	requireExitCode(t, 1, err)
}

func TestStopCommandJSON(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "stop", "--json")
	must.NoError(t, err, "lstk stop --json failed: %s", stderr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "ok", envelope.Status)
	must.Eq(t, "stop", envelope.Command)
	must.Nil(t, envelope.Error)

	var data struct {
		Emulators []struct {
			Type       string `json:"type"`
			Name       string `json:"name"`
			WasRunning bool   `json:"wasRunning"`
		} `json:"emulators"`
	}
	must.NoError(t, json.Unmarshal(envelope.Data, &data))
	must.Len(t, data.Emulators, 1)
	must.Eq(t, "aws", data.Emulators[0].Type)
	must.Eq(t, "localstack-aws", data.Emulators[0].Name)
	must.True(t, data.Emulators[0].WasRunning)
}

func TestStopCommandJSONNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	stdout, _, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "stop", "--json")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "error", envelope.Status)
	must.NotNil(t, envelope.Error)
	must.Eq(t, "EMULATOR_NOT_RUNNING", envelope.Error.Code)
	must.Eq(t, "EMULATOR", envelope.Error.Category)
}
