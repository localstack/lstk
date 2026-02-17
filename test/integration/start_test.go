package integration_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartCommandSucceedsWithValidToken(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.With(env.APIEndpoint, mockServer.URL)
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
	authToken := env.Require(t, env.AuthToken)
	err := SetAuthTokenInKeyring(authToken)
	require.NoError(t, err, "failed to store token in keyring")

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Run without LOCALSTACK_AUTH_TOKEN should use keyring
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL)
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

	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.With(env.AuthToken, "invalid-token").With(env.APIEndpoint, mockServer.URL)
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail with invalid token")
	assert.Contains(t, string(output), "license validation failed")
}

func TestStartCommandDoesNothingWhenAlreadyRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startTestContainer(t, ctx)

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN=fake-token")
	output, err := cmd.CombinedOutput()

	require.NoError(t, err, "lstk start should succeed when container is already running: %s", output)
	assert.Contains(t, string(output), "already running")
}

func TestStartCommandFailsWhenPortInUse(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = append(os.Environ(), "LOCALSTACK_AUTH_TOKEN=fake-token")
	output, err := cmd.CombinedOutput()

	require.Error(t, err, "expected lstk start to fail when port is in use")
	assert.Contains(t, string(output), "port 4566 already in use")
}

func cleanup() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	_ = DeleteAuthTokenFromKeyring()
}
