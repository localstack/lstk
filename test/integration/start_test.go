package integration_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const containerName = "localstack-aws"

func TestStartCommand(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/lstk", "start")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "lstk start failed: %s", output)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")

	assert.True(t, inspect.State.Running, "container is not running, state: %s", inspect.State.Status)
}

func cleanup() {
	ctx := context.Background()
	dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}
