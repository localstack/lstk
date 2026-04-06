package integration_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoctorCommandFailsWhenDockerUnavailable(t *testing.T) {
	analyticsSrv, events := mockAnalyticsServer(t)
	environ := append(env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "DOCKER_HOST=unix:///no/such/docker.sock")

	stdout, stderr, err := runLstk(t, testContext(t), "", environ, "doctor")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Docker runtime")
	assert.Contains(t, stdout, "FAIL")
	assert.Contains(t, stdout, "cannot connect to Docker daemon")
	assertCommandTelemetry(t, events, "doctor", 1)
}

func TestDoctorCommandFailsWhenConfigInvalid(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[[containers]]\ntype = \"aws\"\n"), 0644))

	analyticsSrv, events := mockAnalyticsServer(t)
	environ := append(env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "DOCKER_HOST=unix:///no/such/docker.sock")

	stdout, stderr, err := runLstk(t, testContext(t), "", environ, "--config", configFile, "doctor")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Config file")
	assert.Contains(t, stdout, "port is required")
	assertCommandTelemetry(t, events, "doctor", 1)
}

func TestDoctorCommandDoesNotCreateConfigFile(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
	expectedConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")

	analyticsSrv, events := mockAnalyticsServer(t)
	environ := env.Environ(append(testEnvWithHome(tmpHome, xdgOverride), "DOCKER_HOST=unix:///no/such/docker.sock")).
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

	stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, "doctor")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "doctor is read-only and will not create it")
	assert.NoFileExists(t, expectedConfigFile)
	assertCommandTelemetry(t, events, "doctor", 1)
}

func TestDoctorCommandWarnsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	configFile := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, configFile)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, testContext(t), "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "--config", configFile, "doctor")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Doctor completed with warnings")
	assertCommandTelemetry(t, events, "doctor", 0)
}

func TestDoctorCommandShowsRunningEmulatorHealth(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintln(w, `{"version": "4.14.1", "services": {}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	configFile := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, configFile)

	host := strings.TrimPrefix(server.URL, "http://")
	analyticsSrv, events := mockAnalyticsServer(t)
	environ := env.With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.LocalStackHost, host).
		With(env.AuthToken, "test-token")

	stdout, stderr, err := runLstk(t, ctx, "", environ, "--config", configFile, "doctor")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "LocalStack AWS Emulator")
	assert.Contains(t, stdout, "version 4.14.1")
	assertCommandTelemetry(t, events, "doctor", 0)
}
