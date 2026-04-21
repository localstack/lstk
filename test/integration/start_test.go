package integration_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.NoError(t, err, "lstk start should succeed when container is already running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 0)
}

func TestStartCommandFailsWhenPortInUse(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Simulates port in use by non-LocalStack process (/_localstack/info will fail)
	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	defer func() { _ = ln.Close() }()

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.Error(t, err, "expected lstk start to fail when port is in use")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Port 4566 already in use")
	assert.Contains(t, stdout, "Free the port or configure a different one.")
	assert.Contains(t, stdout, "Use another port in the configuration:")

	// Both lstk_lifecycle (start_error) and lstk_command events should be emitted.
	byName := collectTelemetryByName(t, events, 2)
	assert.Contains(t, byName, "lstk_lifecycle")
	assert.Contains(t, byName, "lstk_command")
}

func TestStartCommandAttachesToExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	// Start a container with a different name to simulate an externally-managed instance.
	startExternalContainer(t, ctx, fakeImage, "localstack-external", "4566")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "start")
	require.NoError(t, err, "lstk start should succeed when external container is running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 0)
}

func TestStartCommandAttachesWhenLocalStackRespondingOnPort(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Serve a mock /_localstack/info on port 4566 so lstk can identify the running version.
	ln, err := net.Listen("tcp", ":4566")
	require.NoError(t, err, "failed to bind port 4566 for test")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"3.4.0","edition":"pro"}`))
			return
		}
		http.NotFound(w, r)
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token"), "start")
	require.NoError(t, err, "lstk start should succeed when LocalStack is already running: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "3.4.0")
	assert.Contains(t, stdout, "already running")
}

func TestStartCommandFailsWhenLocalStackRunningOnDifferentPort(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	// Tag the test image as a LocalStack pro image to simulate an instance running.
	const fakeImage = "localstack/localstack-pro:test-fake"
	require.NoError(t, dockerClient.ImageTag(ctx, testImage, fakeImage))
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, image.RemoveOptions{})
	})

	// Start it on another port
	startExternalContainer(t, ctx, fakeImage, "localstack-external", "4566")

	configContent := `
[[containers]]
type = "aws"
port = "4567"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "start")
	require.Error(t, err, "expected lstk start to fail when LS is already running on a different port")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "already running")
	assertCommandTelemetry(t, events, "start", 1)
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
}

func TestStartCommandSetsUpContainerCorrectly(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	t.Run("environment variables", func(t *testing.T) {
		envVars := containerEnvToMap(inspect.Config.Env)
		assert.Equal(t, ":4566,:443", envVars["GATEWAY_LISTEN"])
		assert.Equal(t, containerName, envVars["MAIN_CONTAINER_NAME"])
		assert.NotEmpty(t, envVars["LOCALSTACK_AUTH_TOKEN"])
	})

	t.Run("docker socket mount", func(t *testing.T) {
		if !strings.HasPrefix(dockerClient.DaemonHost(), "unix://") {
			t.Skip("Docker daemon is not reachable via unix socket")
		}

		assert.True(t, hasBindTarget(inspect.HostConfig.Binds, "/var/run/docker.sock"),
			"expected Docker socket bind mount to /var/run/docker.sock, got: %v", inspect.HostConfig.Binds)
		assert.True(t, hasBindSource(inspect.HostConfig.Binds, "/var/run/docker.sock"),
			"expected Docker socket bind mount from /var/run/docker.sock, got: %v", inspect.HostConfig.Binds)

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
		resp, err := http.Get("http://localhost.localstack.cloud:4566/_localstack/health")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("https health endpoint", func(t *testing.T) {
		// LS certificate is not in system trust store
		// But cert validity is out of scope here: use InsecureSkipVerify
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
		resp, err := client.Get("https://localhost.localstack.cloud/_localstack/health")
		require.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

func TestStartCommandPassesCIAndLocalStackEnvVars(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).
		With(env.CI, "true").
		With(env.DisableEvents, "1"),
		"start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
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

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).
		With(env.Persistence, "1"),
		"start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.State.Running)

	envVars := containerEnvToMap(inspect.Config.Env)
	assert.Equal(t, "1", envVars["LOCALSTACK_PERSISTENCE"])
}

func TestStartCommandSetsPersistenceEnvFromConfig(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

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
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
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

func hasBindSource(binds []string, hostPath string) bool {
	for _, b := range binds {
		parts := strings.Split(b, ":")
		if len(parts) >= 2 && parts[0] == hostPath {
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

func cleanup() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, containerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, containerName, container.RemoveOptions{Force: true})
	_ = DeleteAuthTokenFromKeyring()
}

func cleanupSnowflake() {
	ctx := context.Background()
	_ = dockerClient.ContainerStop(ctx, snowflakeContainerName, container.StopOptions{})
	_ = dockerClient.ContainerRemove(ctx, snowflakeContainerName, container.RemoveOptions{Force: true})
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

func TestStartCommandSucceedsForSnowflake(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	const hostPort = "4567"
	configFile := writeSnowflakeConfig(t, hostPort)

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, snowflakeContainerName)
	require.NoError(t, err, "failed to inspect snowflake container")
	require.True(t, inspect.State.Running, "snowflake container should be running")
	assert.Contains(t, inspect.Config.Image, "localstack/snowflake",
		"expected localstack/snowflake image, got %s", inspect.Config.Image)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/_localstack/health", hostPort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStartCommandFailsForSnowflakeWithoutAddon(t *testing.T) {
	requireDocker(t)
	cleanupSnowflake()
	t.Cleanup(cleanupSnowflake)

	// License response without the Snowflake add-on product.
	mockServer := createMockLicenseServerWithBody(`{"license_type":"ultimate","products":[]}`)
	defer mockServer.Close()

	configFile := writeSnowflakeConfig(t, "4567")

	_, stderr, err := runLstk(t, testContext(t), "", env.With(env.AuthToken, "fake-token").With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.Error(t, err, "expected lstk start to fail when Snowflake add-on is not in license")
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "subscription does not include the Snowflake emulator")
}

