package integration_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStopCommandSucceeds(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	stopCmd := exec.CommandContext(ctx, binaryPath(), "stop")
	output, err := stopCmd.CombinedOutput()
	require.NoError(t, err, "lstk stop failed: %s", output)

	outputStr := string(output)
	assert.Contains(t, outputStr, "Stopping", "should show stopping message")
	assert.Contains(t, outputStr, "stopped", "should show stopped message")

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	assert.Error(t, err, "container should not exist after stop")
}

func TestStopCommandFailsWhenNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "stop")
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk stop to fail when container not running")
	assert.Contains(t, string(output), "is not running")
}

func TestStopCommandIsIdempotent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	stopCmd := exec.CommandContext(ctx, binaryPath(), "stop")
	output, err := stopCmd.CombinedOutput()
	require.NoError(t, err, "first lstk stop failed: %s", output)

	_, err = dockerClient.ContainerInspect(ctx, containerName)
	require.Error(t, err, "container should not exist after first stop")

	stopCmd2 := exec.CommandContext(ctx, binaryPath(), "stop")
	_, err = stopCmd2.CombinedOutput()
	assert.Error(t, err, "second lstk stop should fail since container already removed")
}
