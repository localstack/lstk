package integration_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const snowflakeContainerName = "localstack-snowflake"

func TestStartCommandSucceedsWithValidToken(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestStartCommandSucceedsWithKeyringToken(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)
	te := envWithDockerHostFull(t, daemon)

	// Store token in keyring before running command. Token written into the
	// per-test HOME (file keyring) so it's reachable by the subprocess.
	authToken := env.Require(t, env.AuthToken)
	require.NoError(t, writeFileKeyringToken(te.Home, authToken))

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", te.BaseEnv.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.State.Running, "container should be running")
}

func TestStartCommandFailsWithInvalidToken(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)

	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	_, stderr, err := runLstk(t, testContext(t), "", envWithDockerHost(t, daemon).With(env.AuthToken, "invalid-token").With(env.APIEndpoint, mockServer.URL), "start")
	require.Error(t, err, "expected lstk start to fail with invalid token")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "license validation failed")
}

func TestStartCommandDoesNothingWhenAlreadyRunning(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)

	ctx := testContext(t)
	startStubInDind(t, daemon, containerName)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.NoError(t, err, "lstk start should succeed when container is already running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 0)
}

func TestStartCommandFailsWhenPortInUse(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)

	// Simulate "port in use by non-LocalStack" by running netcat inside dind on
	// 4566. lstk inside dind will see the port held but no /_localstack/info
	// response.
	const blocker = "port-blocker"
	resp, err := daemon.Client.ContainerCreate(ctx,
		&container.Config{
			Image:        testImage,
			Cmd:          []string{"nc", "-lk", "-p", "4566"},
			ExposedPorts: nat.PortSet{"4566/tcp": struct{}{}},
		},
		&container.HostConfig{
			PortBindings: nat.PortMap{"4566/tcp": []nat.PortBinding{{HostPort: "4566"}}},
		},
		nil, nil, blocker,
	)
	require.NoError(t, err, "failed to create port-blocker container")
	require.NoError(t, daemon.Client.ContainerStart(ctx, resp.ID, container.StartOptions{}))

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, testContext(t), "", envWithDockerHost(t, daemon).With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.Error(t, err, "expected lstk start to fail when port is in use")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Port 4566 already in use")
	assert.Contains(t, stdout, "Free the port or configure a different one.")
	assert.Contains(t, stdout, "Use another port in the configuration:")

	byName := collectTelemetryByName(t, events, 2)
	assert.Contains(t, byName, "lstk_lifecycle")
	assert.Contains(t, byName, "lstk_command")
}

func TestStartCommandAttachesToExternalContainer(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, daemon.Client.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = daemon.Client.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	startExternalInDind(t, daemon, fakeImage, "localstack-external", "4566")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.NoError(t, err, "lstk start should succeed when external container is running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 0)
}

func TestStartCommandAttachesWhenLocalStackRespondingOnPort(t *testing.T) {
	// TODO(dind): requires an HTTP responder running inside dind on 4566 so
	// lstk can fetch /_localstack/info. Stand up via a small responder image
	// or python3 -m http.server with a static JSON file.
	t.Skip("TODO: rewrite for dind — needs HTTP responder inside dind")
}

func TestStartCommandFailsWhenLocalStackRunningOnDifferentPort(t *testing.T) {
	requireDocker(t)
	t.Parallel()
	daemon := startEphemeralDocker(t)
	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, daemon.Client.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = daemon.Client.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	startExternalInDind(t, daemon, fakeImage, "localstack-external", "4566")

	configContent := `
[[containers]]
type = "aws"
port = "4567"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "start")
	require.Error(t, err, "expected lstk start to fail when LS is already running on a different port")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 1)
}

func TestStartCommandSucceedsWithNonDefaultPort(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

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
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
}

func TestStartCommandSetsUpContainerCorrectly(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	t.Run("environment variables", func(t *testing.T) {
		envVars := containerEnvToMap(inspect.Config.Env)
		assert.Equal(t, ":4566,:443", envVars["GATEWAY_LISTEN"])
		assert.Equal(t, containerName, envVars["MAIN_CONTAINER_NAME"])
		assert.NotEmpty(t, envVars["LOCALSTACK_AUTH_TOKEN"])
	})

	t.Run("docker socket mount", func(t *testing.T) {
		// Inside dind, lstk sees a unix socket at /var/run/docker.sock.
		assert.True(t, hasBindTarget(inspect.HostConfig.Binds, "/var/run/docker.sock"),
			"expected Docker socket bind mount to /var/run/docker.sock, got: %v", inspect.HostConfig.Binds)
		envVars := containerEnvToMap(inspect.Config.Env)
		assert.Equal(t, "unix:///var/run/docker.sock", envVars["DOCKER_HOST"])
	})

	t.Run("service port range", func(t *testing.T) {
		for p := 4510; p <= 4559; p++ {
			port := nat.Port(fmt.Sprintf("%d/tcp", p))
			bindings := inspect.HostConfig.PortBindings[port]
			if assert.NotEmpty(t, bindings, "port %d/tcp should be bound", p) {
				assert.Equal(t, strconv.Itoa(p), bindings[0].HostPort)
			}
		}
	})

	t.Run("main port", func(t *testing.T) {
		mainBindings := inspect.HostConfig.PortBindings[nat.Port("4566/tcp")]
		require.NotEmpty(t, mainBindings, "port 4566/tcp should be bound")
		assert.Equal(t, "4566", mainBindings[0].HostPort)
	})

	t.Run("https port", func(t *testing.T) {
		httpsBindings := inspect.HostConfig.PortBindings[nat.Port("443/tcp")]
		require.NotEmpty(t, httpsBindings, "port 443/tcp should be bound")
		assert.Equal(t, "443", httpsBindings[0].HostPort)
	})

	t.Run("volume mount", func(t *testing.T) {
		assert.True(t, hasBindTarget(inspect.HostConfig.Binds, "/var/lib/localstack"),
			"expected volume bind mount to /var/lib/localstack, got: %v", inspect.HostConfig.Binds)
	})

	t.Run("http health endpoint", func(t *testing.T) {
		url := fmt.Sprintf("http://127.0.0.1:%d/_localstack/health", daemon.hostPortFor(4566))
		resp, err := http.Get(url)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("https health endpoint", func(t *testing.T) {
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		url := fmt.Sprintf("https://127.0.0.1:%d/_localstack/health", daemon.hostPortFor(443))
		resp, err := client.Get(url)
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestStartCommandPassesCIAndLocalStackEnvVars(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL).
		With(env.CI, "true").
		With(env.DisableEvents, "1"),
		"start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	envVars := containerEnvToMap(inspect.Config.Env)
	assert.Equal(t, "true", envVars["CI"])
	assert.Equal(t, "1", envVars["LOCALSTACK_DISABLE_EVENTS"])
	assert.NotEmpty(t, envVars["LOCALSTACK_AUTH_TOKEN"])
}

func TestStartCommandForwardsPersistenceEnvFromHost(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL).
		With(env.Persistence, "1"),
		"start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	envVars := containerEnvToMap(inspect.Config.Env)
	assert.Equal(t, "1", envVars["LOCALSTACK_PERSISTENCE"])
}

func TestStartCommandSetsPersistenceEnvFromConfig(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	configContent := `
[env.persistence]
PERSISTENCE = "1"

[[containers]]
type = "aws"
tag = "latest"
port = "4566"
env = ["persistence"]
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	envVars := containerEnvToMap(inspect.Config.Env)
	assert.Equal(t, "1", envVars["PERSISTENCE"])
}

// hasBindTarget checks if any bind mount targets the given container path.
func hasBindTarget(binds []string, containerPath string) bool {
	for _, b := range binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 && parts[1] == containerPath {
			return true
		}
	}
	return false
}

// containerEnvToMap converts a Docker container's []string env to a map.
func containerEnvToMap(envList []string) map[string]string {
	m := make(map[string]string, len(envList))
	for _, e := range envList {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}
	return m
}

func writeSnowflakeConfig(t *testing.T, hostPort string) string {
	t.Helper()
	content := fmt.Sprintf(`
[[containers]]
type = "snowflake"
tag  = "latest"
port = %q
`, hostPort)
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0644))
	return configFile
}

func TestStartCommandForSnowflakeSkipsLicenseValidation(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, snowflakeImage)

	// Mock server that rejects all license requests — this would cause lstk start to fail for AWS.
	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "--config", writeSnowflakeConfig(t, "4566"), "start")
	require.NoError(t, err, "lstk start should succeed for snowflake even when the license server rejects the request: %s", stderr)
	requireExitCode(t, 0, err)
}

func TestStartCommandSucceedsForSnowflake(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, snowflakeImage)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	const hostPort = "4566"
	configFile := writeSnowflakeConfig(t, hostPort)

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", envWithDockerHost(t, daemon).With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := daemon.Client.ContainerInspect(ctx, snowflakeContainerName)
	require.NoError(t, err, "failed to inspect snowflake container")
	require.True(t, inspect.State.Running, "snowflake container should be running")
	assert.Contains(t, inspect.Config.Image, "localstack/snowflake",
		"expected localstack/snowflake image, got %s", inspect.Config.Image)

	url := fmt.Sprintf("http://127.0.0.1:%d/_localstack/health", daemon.hostPortFor(4566))
	resp, err := http.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
