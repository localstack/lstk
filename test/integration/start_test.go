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
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "localstack"
	keyringUser    = "auth-token"
)

const containerName = "localstack-aws"

func TestStartCommandSucceedsWithValidToken(t *testing.T) {
	requireDocker(t)
	authToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	require.NotEmpty(t, authToken, "LOCALSTACK_AUTH_TOKEN must be set to run this test")

	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN="+authToken)
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start failed: %s", output)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestStartCommandSucceedsWithKeyringToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Store token in keyring before running command
	authToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	require.NotEmpty(t, authToken, "LOCALSTACK_AUTH_TOKEN must be set to run this test")
	err := keyring.Set(keyringService, keyringUser, authToken)
	require.NoError(t, err, "failed to store token in keyring")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Run without LOCALSTACK_AUTH_TOKEN should use keyring
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = envWithoutAuthToken()
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start failed: %s", output)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestStartCommandFailsWithInvalidToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN=invalid-token")
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail with invalid token")
	assert.Contains(t, string(output), "License activation failed")
}

func cleanup() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	_ = keyring.Delete(keyringService, keyringUser)
}
