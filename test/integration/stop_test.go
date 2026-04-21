package integration_test

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/image"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopCommandSucceeds(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "stop")
	require.NoError(t, err, "lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Stopping", "should show stopping message")
	assert.Contains(t, stdout, "stopped", "should show stopped message")

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	assert.Error(t, err, "container should not exist after stop")

	// Both lstk_lifecycle (stop) and lstk_command events should be emitted.
	byName := collectTelemetryByName(t, events, 2)
	assert.Contains(t, byName, "lstk_lifecycle")
	assert.Contains(t, byName, "lstk_command")
}

func TestStopCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	analyticsSrv, events := mockAnalyticsServer(t)
	_, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "stop")
	require.Error(t, err, "expected lstk stop to fail when container not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "is not running")
	assertCommandTelemetry(t, events, "stop", 1)
}

func TestStopCommandStopsExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-external", "4566")

	stdout, stderr, err := runLstk(t, ctx, "", nil, "stop")
	require.NoError(t, err, "lstk stop should stop external container: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "stopped")

	_, err = dockerClient.ContainerInspect(ctx, "localstack-external")
	assert.Error(t, err, "external container should be gone after lstk stop")
}

func TestStopCommandIsIdempotent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	_, stderr, err := runLstk(t, ctx, "", nil, "stop")
	require.NoError(t, err, "first lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	require.Error(t, err, "container should not exist after first stop")

	_, _, err = runLstk(t, ctx, "", nil, "stop")
	assert.Error(t, err, "second lstk stop should fail since container already removed")
	requireExitCode(t, 1, err)
}
