package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopCommandSucceeds(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, stderr, err := runLstk(t, ctx, "", nil, "stop")
	require.NoError(t, err, "lstk stop failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Stopping", "should show stopping message")
	assert.Contains(t, stdout, "stopped", "should show stopped message")

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	assert.Error(t, err, "container should not exist after stop")
}

func TestStopCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	_, stderr, err := runLstk(t, testContext(t), "", nil, "stop")
	require.Error(t, err, "expected lstk stop to fail when container not running")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "is not running")
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
