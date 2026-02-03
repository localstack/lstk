package integration_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const containerName = "localstack-aws"

func TestStartCommandSucceedsWithValidToken(t *testing.T) {
	authToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	require.NotEmpty(t, authToken, "LOCALSTACK_AUTH_TOKEN must be set to run this test")

	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/lstk", "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN="+authToken)
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start failed: %s", output)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestStartCommandFailsWithoutToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/lstk", "start")
	cmd.Env = []string{} // Clear environment to ensure no token
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail without token")
	assert.Contains(t, string(output), "auth token not found")
}

func TestStartCommandFailsWithInvalidToken(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "../../bin/lstk", "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN=invalid-token")
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail with invalid token")
	assert.Contains(t, string(output), "License activation failed")
}

func cleanup() {
	ctx := context.Background()
	dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
}
