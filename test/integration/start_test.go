package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running")

	assert.NotContains(t, stdout, "• Persistence:",
		"persistence bullet must be omitted when --persist is not set")
}

// PRO-323: a pinned image already present locally must be reused, not re-pulled.
// Tags the lightweight test image under a pinned localstack-pro tag so the image
// is present locally; lstk should skip the pull and emit "Using local image".
// We only assert the pull decision (emitted before the container starts) — the
// stand-in image is not a real emulator, so the subsequent start fails fast when
// it exits. A dedicated host port keeps this off the shared 4566 used by the
// other container tests.
func TestStartCommandReusesLocalImageWhenPresent(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const pinnedTag = "reuse-local-test"
	const pinnedImage = "localstack/localstack-pro:" + pinnedTag
	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: pinnedImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), pinnedImage, client.ImageRemoveOptions{})
	})

	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile,
		[]byte(fmt.Sprintf("[[containers]]\ntype = \"aws\"\ntag = %q\nport = \"4599\"\n", pinnedTag)), 0644))

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e := append(testEnvWithHome(home, ""),
		string(env.APIEndpoint)+"="+mockServer.URL,
		string(env.AuthToken)+"=fake-token")
	stdout, _, _ := runLstk(t, ctx, "", e, "--config", configFile, "start")

	assert.Contains(t, stdout, "Using local image "+pinnedImage, "a pinned image present locally should be reused")
	assert.NotContains(t, stdout, "Pulling", "lstk must not re-pull an image that is already present")
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

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running")
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

func TestStartDoesNotHangWithExternalContainerAndNoCachedLabel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-external", "4566")

	// Fresh HOME = no cached plan_label. --config prevents firstRun=true, which would
	// trigger emulator selection and block on keyboard input.
	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	stdout, err := runLstkInPTY(t, ctx,
		env.With(env.AuthToken, "fake-token").With(env.Home, home),
		"start", "--config", configFile,
	)
	require.NoError(t, err, "lstk start hung: TUI did not exit when external container was running and no plan label was cached")
	assert.Contains(t, stdout, "already running")
}

func TestStartCommandAttachesToExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
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
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
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

func TestStartCommandFailsOnEmulatorTypeMismatch(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)

	// Tag the test image as a LocalStack pro image to simulate AWS LocalStack running.
	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-external-aws", "4566")

	// Start lstk with a Snowflake config on the same port — expect a clear mismatch error.
	configFile := writeSnowflakeConfig(t, "4566")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, _, err := runLstk(t, ctx, "", env.With(env.AuthToken, "fake-token").With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "start")
	require.Error(t, err, "lstk start should fail on emulator type mismatch")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "LocalStack AWS Emulator is running on port 4566")
	assert.Contains(t, stdout, "Your config specifies the LocalStack Snowflake Emulator")
	assert.Contains(t, stdout, "docker stop localstack-external-aws")

	byName := collectTelemetryByName(t, events, 2)
	cmdPayload, _ := byName["lstk_command"]["payload"].(map[string]any)
	cmdParams, _ := cmdPayload["parameters"].(map[string]any)
	cmdResult, _ := cmdPayload["result"].(map[string]any)
	assert.Equal(t, "start", cmdParams["command"])
	assert.InDelta(t, 1, cmdResult["exit_code"], 0)

	lifecycle, ok := byName["lstk_lifecycle"]
	require.True(t, ok, "expected lstk_lifecycle telemetry event")
	lifePayload, _ := lifecycle["payload"].(map[string]any)
	assert.Equal(t, "start_error", lifePayload["event_type"])
	assert.Equal(t, "emulator_mismatch", lifePayload["error_code"])
	assert.Equal(t, "snowflake", lifePayload["emulator"])
	assert.Contains(t, lifePayload["error_msg"], "running aws on port 4566, configured snowflake")
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

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "localhost.localstack.cloud:4567", envVars["LOCALSTACK_HOST"],
		"LOCALSTACK_HOST must reflect configured host port so LocalStack accepts requests on it")
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

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running)

	t.Run("environment variables", func(t *testing.T) {
		envVars := containerEnvToMap(inspect.Container.Config.Env)
		assert.Equal(t, ":4566,:443", envVars["GATEWAY_LISTEN"])
		assert.Equal(t, containerName, envVars["MAIN_CONTAINER_NAME"])
		assert.Equal(t, "localhost.localstack.cloud:4566", envVars["LOCALSTACK_HOST"])
		assert.NotEmpty(t, envVars["LOCALSTACK_AUTH_TOKEN"])
	})

	t.Run("docker socket mount", func(t *testing.T) {
		if !strings.HasPrefix(dockerClient.DaemonHost(), "unix://") {
			t.Skip("Docker daemon is not reachable via unix socket")
		}

		assert.True(t, hasBindTarget(inspect.Container.HostConfig.Binds, "/var/run/docker.sock"),
			"expected Docker socket bind mount to /var/run/docker.sock, got: %v", inspect.Container.HostConfig.Binds)
		assert.True(t, hasBindSource(inspect.Container.HostConfig.Binds, "/var/run/docker.sock"),
			"expected Docker socket bind mount from /var/run/docker.sock, got: %v", inspect.Container.HostConfig.Binds)

		envVars := containerEnvToMap(inspect.Container.Config.Env)
		assert.Equal(t, "unix:///var/run/docker.sock", envVars["DOCKER_HOST"])
	})

	t.Run("service port range", func(t *testing.T) {
		for p := 4510; p <= 4559; p++ {
			port := network.MustParsePort(fmt.Sprintf("%d/tcp", p))
			bindings := inspect.Container.HostConfig.PortBindings[port]
			if assert.NotEmpty(t, bindings, "port %d/tcp should be bound", p) {
				assert.Equal(t, strconv.Itoa(p), bindings[0].HostPort)
			}
		}
	})

	t.Run("main port", func(t *testing.T) {
		mainBindings := inspect.Container.HostConfig.PortBindings[network.MustParsePort("4566/tcp")]
		require.NotEmpty(t, mainBindings, "port 4566/tcp should be bound")
		assert.Equal(t, "4566", mainBindings[0].HostPort)
	})

	t.Run("https port", func(t *testing.T) {
		httpsBindings := inspect.Container.HostConfig.PortBindings[network.MustParsePort("443/tcp")]
		require.NotEmpty(t, httpsBindings, "port 443/tcp should be bound")
		assert.Equal(t, "443", httpsBindings[0].HostPort)
	})

	t.Run("volume mount", func(t *testing.T) {
		assert.True(t, hasBindTarget(inspect.Container.HostConfig.Binds, "/var/lib/localstack"),
			"expected volume bind mount to /var/lib/localstack, got: %v", inspect.Container.HostConfig.Binds)
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

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "true", envVars["CI"])
	assert.Equal(t, "1", envVars["LOCALSTACK_DISABLE_EVENTS"])
	assert.NotEmpty(t, envVars["LOCALSTACK_AUTH_TOKEN"])
}

func TestStartCommandPersistFlagSetsPersistenceEnv(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "start", "--persist")
	require.NoError(t, err, "lstk start --persist failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "1", envVars["LOCALSTACK_PERSISTENCE"])

	assert.Contains(t, stdout, "• Persistence: Enabled",
		"lstk start --persist should surface persistence state in the header")

	statusStdout, statusStderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "status")
	require.NoError(t, err, "lstk status failed: %s", statusStderr)
	assert.Contains(t, statusStdout, "• Persistence: Enabled",
		"lstk status should surface persistence state when the running container has it enabled")
}

func TestStartCommandForwardsPersistenceEnvFromHost(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).
		With(env.Persistence, "1"),
		"start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "1", envVars["LOCALSTACK_PERSISTENCE"])

	assert.Contains(t, stdout, "• Persistence: Enabled",
		"lstk start should surface persistence state when LOCALSTACK_PERSISTENCE=1 is set in the shell")
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
LOCALSTACK_PERSISTENCE = "1"

[[containers]]
type = "aws"
tag = "latest"
port = "4566"
env = ["persistence"]
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running)

	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "1", envVars["LOCALSTACK_PERSISTENCE"])

	assert.Contains(t, stdout, "• Persistence: Enabled",
		"lstk start should surface persistence state when LOCALSTACK_PERSISTENCE=1 is set in the config profile")
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

func TestStartHidesHeaderUntilAuthComplete(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	output := &syncBuffer{}
	go func() {
		_, _ = io.Copy(output, ptmx)
	}()

	// Wait for the login prompt — header must not be visible yet.
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("Press any key when complete"))
	}, 10*time.Second, 100*time.Millisecond, "auth prompt should appear")

	assert.NotContains(t, output.String(), "lstk ", "header must be hidden while auth is pending")

	// Complete auth by pressing ENTER.
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	// After auth completes, the header must appear. Look for the header's
	// "lstk " prefix — the version that follows is wrapped in ANSI styling
	// so a contiguous "lstk (" match would fail under terminal rendering.
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("lstk "))
	}, 10*time.Second, 100*time.Millisecond, "header should appear after auth completes")

	cancel()
	_ = cmd.Wait()
}

// TestStartWithCustomImageFailsClearlyWhenUnavailable verifies that a configured
// custom image is honored and that, when it can be neither pulled nor found
// locally, the start fails with the pull error rather than hanging. The "latest"
// tag defers the license check until after the pull, so the (unreachable) license
// endpoint is never contacted — the pull failure surfaces first.
func TestStartWithCustomImageFailsClearlyWhenUnavailable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
image = "lstk-nonexistent-custom-image"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	// A dummy token satisfies the up-front auth check (it is not validated here);
	// the flow fails when the custom image cannot be pulled or found locally.
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "dummy-token")
	stdout, stderr, err := runLstk(t, testContext(t), "", e, "--config", configFile, "--non-interactive", "start")

	require.Error(t, err, "expected start to fail when the custom image is unavailable")
	requireExitCode(t, 1, err)
	combined := stdout + stderr
	assert.Contains(t, combined, "Failed to pull lstk-nonexistent-custom-image:latest")
}

// TestStartFallsBackToLocalImageWhenPullFails verifies the offline degradation
// path for image pulls: when the configured image cannot be pulled (registry
// unreachable, or the image was never published) but is already present locally,
// lstk warns and starts the local image instead of failing.
//
// The scenario is reproduced without cutting off the network by tagging a real
// LocalStack image under a name no registry can serve: the pull fails, but
// ImageExists reports the image locally, so the fallback fires. A valid token is
// still required for the (real) container to activate and become healthy.
func TestStartFallsBackToLocalImageWhenPullFails(t *testing.T) {
	requireDocker(t)
	authToken := env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const sourceImage = "localstack/localstack-pro:latest"
	const localImage = "lstk-offline-fallback-test"
	reader, err := dockerClient.ImagePull(ctx, sourceImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull source image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()

	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: sourceImage, Target: localImage + ":latest"})
	require.NoError(t, err, "failed to tag local image")
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), localImage+":latest", client.ImageRemoveOptions{Force: true})
	})

	// The started container writes root-owned files into its volume dir; keep that
	// dir outside t.TempDir (whose cleanup runs as the unprivileged test user and
	// would fail on root-owned files) so HOME can stay fully isolated below.
	volumeDir, err := os.MkdirTemp("", "lstk-volume")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(volumeDir) }) // best-effort; root-owned files may remain

	home := t.TempDir()
	configContent := fmt.Sprintf(`
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
image = %q
volume = %q
`, localImage, volumeDir)
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, mockServer.URL).
		With(env.AuthToken, authToken)
	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start")
	require.NoError(t, err, "lstk start should fall back to the local image: %s", stderr)
	requireExitCode(t, 0, err)

	assert.Contains(t, stdout+stderr, "using the local image", "expected the local-image fallback warning")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running from the local image")
}

// TestStartContinuesWhenLicenseServerUnreachable verifies the offline degradation
// path for license validation: when the license server cannot be reached — a
// transport-level failure (offline/proxy/cert), not a definitive rejection — lstk
// skips the pre-flight check and lets the container validate its own bundled
// license instead of blocking the start.
//
// The endpoint is made unreachable by closing the mock server immediately, so the
// pre-flight request is refused at the transport level rather than returning an
// *api.LicenseError. A "latest" tag defers validation until after the (successful)
// pull, so the unreachable endpoint is hit at the post-pull check.
func TestStartContinuesWhenLicenseServerUnreachable(t *testing.T) {
	requireDocker(t)
	authToken := env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	unreachable := createMockLicenseServer(true)
	unreachableURL := unreachable.URL
	unreachable.Close()

	// The started container writes root-owned files into its volume dir; keep that
	// dir outside t.TempDir (whose cleanup runs as the unprivileged test user and
	// would fail on root-owned files) so HOME can stay fully isolated below.
	volumeDir, err := os.MkdirTemp("", "lstk-volume")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(volumeDir) }) // best-effort; root-owned files may remain

	home := t.TempDir()
	configContent := fmt.Sprintf(`
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = %q
`, volumeDir)
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	e := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, unreachableURL).
		With(env.AuthToken, authToken)
	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start")
	require.NoError(t, err, "lstk start should continue when the license server is unreachable: %s", stderr)
	requireExitCode(t, 0, err)

	assert.Contains(t, stdout+stderr, "Could not reach the license server", "expected the license-unreachable warning")

	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	assert.True(t, inspect.Container.State.Running, "container should be running")
}

// TestStartUsesLocalCustomImageWithoutPullOrLicenseCheck verifies the offline
// success path from the #325 review: when a custom image is configured with a
// pinned tag and is already present locally, lstk starts it with no pull and no
// CLI license check at all. Covers all four points: image set in config, found
// locally and started, no image pulled, no license call from the CLI.
//
// This is intentionally a small, token-free test: the custom image is a
// lightweight stand-in tagged locally (so it exits right after it is created),
// which lets us assert the pull/license decisions and that the container lstk
// created uses the local image — without a real auth token or a reachable
// registry/license server. A real container reaching a healthy state from a
// local image is already covered by TestStartFallsBackToLocalImageWhenPullFails.
func TestStartUsesLocalCustomImageWithoutPullOrLicenseCheck(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const customImage = "lstk-offline-only-image"
	const pinnedTag = "1.0.0"
	const fullRef = customImage + ":" + pinnedTag
	// A pinned tag names the container "localstack-aws-<tag>", not the bare
	// "localstack-aws" that the shared cleanup() removes.
	const wantContainer = "localstack-aws-" + pinnedTag

	// Make the custom image present locally without a registry by tagging the
	// lightweight test image under it.
	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fullRef})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fullRef, client.ImageRemoveOptions{Force: true})
	})

	// The pinned-tag container isn't the bare "localstack-aws" that cleanup()
	// removes, so remove it explicitly to avoid leaking it onto port 4566.
	removeContainer := func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), wantContainer, client.ContainerRemoveOptions{Force: true})
	}
	removeContainer()
	t.Cleanup(removeContainer)

	// Any request to the license server fails the test: a local pinned image must
	// not trigger a CLI license check.
	var licenseHits int32
	licenseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&licenseHits, 1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer licenseServer.Close()

	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	configContent := fmt.Sprintf(`
[[containers]]
type = "aws"
tag = %q
port = "4566"
image = %q
`, pinnedTag, customImage)
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	// A dummy token satisfies the up-front auth check; it is never validated
	// because the license pre-flight is skipped for a local image.
	e := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, licenseServer.URL).
		With(env.AuthToken, "dummy-token")
	stdout, stderr, _ := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start")
	combined := stdout + stderr

	// Found locally and used — nothing is pulled.
	assert.Contains(t, combined, "Using local image "+fullRef,
		"the configured custom image, present locally, should be reused: %s", combined)
	assert.NotContains(t, combined, "Pulling",
		"lstk must not pull when the configured custom image is already present locally")

	// No license check from the CLI for a local image.
	assert.Equal(t, int32(0), atomic.LoadInt32(&licenseHits),
		"the CLI must not contact the license server for a local image")
	assert.NotContains(t, combined, "Checking license",
		"lstk must not run a pre-flight license check for a local image")

	// Started from the configured local image: lstk created the container using it.
	inspect, err := dockerClient.ContainerInspect(ctx, wantContainer, client.ContainerInspectOptions{})
	require.NoError(t, err, "lstk should have created a container from the custom image")
	assert.Equal(t, fullRef, inspect.Container.Config.Image,
		"the container should be created from the configured custom image")
}

func cleanup() {
	ctx := context.Background()
	// ContainerRemove with Force already SIGKILLs the container; an explicit
	// ContainerStop first would add the default 10s SIGTERM grace period.
	_, _ = dockerClient.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})
	_ = DeleteAuthTokenFromKeyring()
}

func cleanupSnowflake() {
	ctx := context.Background()
	_, _ = dockerClient.ContainerRemove(ctx, snowflakeContainerName, client.ContainerRemoveOptions{Force: true})
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

	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	// Mock server that rejects all license requests — this would cause lstk start to fail for AWS.
	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", writeSnowflakeConfig(t, "4566"), "start")
	require.NoError(t, err, "lstk start should succeed for snowflake even when the license server rejects the request: %s", stderr)
	requireExitCode(t, 0, err)
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

	const hostPort = "4566"
	configFile := writeSnowflakeConfig(t, hostPort)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, snowflakeContainerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect snowflake container")
	require.True(t, inspect.Container.State.Running, "snowflake container should be running")
	assert.Contains(t, inspect.Container.Config.Image, "localstack/snowflake",
		"expected localstack/snowflake image, got %s", inspect.Container.Config.Image)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/_localstack/health", hostPort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Contains(t, stdout, "• Snowflake endpoint: http://snowflake.",
		"snowflake start should print the snowflake-prefixed endpoint hint")
	assert.NotContains(t, stdout, "• Endpoint: localhost.localstack.cloud",
		"snowflake start should not print the bare AWS-style endpoint line")
	assert.Contains(t, stdout, "> Tip:",
		"snowflake start should print a tip line like AWS does")
}

func TestStartCommandSetsSnowflakeS3EndpointFromPort(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	const hostPort = "4599"
	configFile := writeSnowflakeConfig(t, hostPort)

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, snowflakeContainerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect snowflake container")
	envVars := containerEnvToMap(inspect.Container.Config.Env)
	assert.Equal(t, "s3.localhost.localstack.cloud:"+hostPort, envVars["SF_S3_ENDPOINT"],
		"SF_S3_ENDPOINT should match the configured Snowflake port")
}

const azureContainerName = "localstack-azure"

func cleanupAzure() {
	ctx := context.Background()
	_, _ = dockerClient.ContainerRemove(ctx, azureContainerName, client.ContainerRemoveOptions{Force: true})
}

func writeAzureConfig(t *testing.T, hostPort string) string {
	t.Helper()
	content := fmt.Sprintf(`
[[containers]]
type = "azure"
tag  = "latest"
port = %q
`, hostPort)
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(content), 0644))
	return configFile
}

func TestStartCommandSucceedsForAzure(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	const hostPort = "4566"
	configFile := writeAzureConfig(t, hostPort)

	ctx := testContext(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, azureContainerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect azure container")
	require.True(t, inspect.Container.State.Running, "azure container should be running")
	assert.Contains(t, inspect.Container.Config.Image, "localstack/localstack-azure",
		"expected localstack/localstack-azure image, got %s", inspect.Container.Config.Image)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/_localstack/health", hostPort))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Contains(t, stdout, "> Tip:",
		"azure start should print a tip line like AWS does")
}

func TestStartCommandForAzureSkipsLicenseValidation(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	// Mock server that rejects all license requests — this would cause lstk start to fail for AWS.
	// Azure activates its own license against the licensing server, so lstk must skip the pre-flight check.
	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", writeAzureConfig(t, "4566"), "start")
	require.NoError(t, err, "lstk start should succeed for azure even when the license server rejects the request: %s", stderr)
	requireExitCode(t, 0, err)
}
