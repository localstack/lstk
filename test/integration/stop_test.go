package integration_test

import (
	"context"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testImage = "alpine:latest"

func startTestContainer(t *testing.T, ctx context.Context) {
	t.Helper()

	reader, err := dockerClient.ImagePull(ctx, testImage, image.PullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	resp, err := dockerClient.ContainerCreate(ctx, &container.Config{
		Image: testImage,
		Cmd:   []string{"sleep", "infinity"},
	}, nil, nil, nil, containerName)
	require.NoError(t, err, "failed to create test container")
	err = dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err, "failed to start test container")
}

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
