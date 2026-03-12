package integration_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

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

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

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

	ctx := testContext(t)
	// Run without LOCALSTACK_AUTH_TOKEN should use keyring
	_, stderr, err := runLstk(t, ctx, "", env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

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

	_, stderr, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "invalid-token").With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail with invalid token")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "license validation failed")
}

func TestStartCommandDoesNothingWhenAlreadyRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token"), "start")
	require.NoError(t, err, "lstk start should succeed when container is already running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already running")
}

func TestStartCommandFailsWhenPortInUse(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	defer func() { _ = ln.Close() }()

	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token"), "start")
	require.Error(t, err, "expected lstk start to fail when port is in use")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Port 4566 already in use")
	assert.Contains(t, stdout, "LocalStack may already be running.")
	assert.Contains(t, stdout, "lstk stop")
}

func TestStartCommandSucceedsWithNonDefaultPort(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4567"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	// lstk start only returns once the health check passes, but verify directly
	// that LocalStack is reachable on the non-default port.
	resp, err := http.Get("http://localhost:4567/_localstack/health")
	require.NoError(t, err, "LocalStack health endpoint not reachable on port 4567")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "LocalStack should be ready on port 4567")
}

func cleanup() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	_ = DeleteAuthTokenFromKeyring()
}
