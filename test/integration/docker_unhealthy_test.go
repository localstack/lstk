package integration_test

import (
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unhealthyDockerEnv returns an environment with DOCKER_HOST pointing to a
// non-existent Unix socket so that the Docker health check fails.
func unhealthyDockerEnv() env.Environ {
	return env.With(env.Key("DOCKER_HOST"), "unix:///var/run/docker-does-not-exist.sock")
}

func TestStartShowsDockerErrorWhenUnhealthy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", unhealthyDockerEnv(), "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Docker is not available")
	assert.Contains(t, stdout, "Install Docker:")
}

func TestStopShowsDockerErrorWhenUnhealthy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", unhealthyDockerEnv(), "stop")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Docker is not available")
	assert.Contains(t, stdout, "Install Docker:")
}

// TestStopJSONIncludesDockerErrorDetail covers PR #374's report that --json
// kept only the "Docker is not available" headline and silently dropped the
// underlying diagnostic (the Summary set in internal/runtime/docker.go) that
// plain text shows alongside it.
func TestStopJSONIncludesDockerErrorDetail(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", unhealthyDockerEnv(), "stop", "--json")
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	require.NotNil(t, envelope.Error)
	assert.Equal(t, "RUNTIME_UNAVAILABLE", envelope.Error.Code)
	assert.Equal(t, "Docker is not available", envelope.Error.Message)
	summary, _ := envelope.Error.Details["summary"].(string)
	assert.Contains(t, summary, "cannot connect to Docker daemon")
}

func TestStatusShowsDockerErrorWhenUnhealthy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", unhealthyDockerEnv(), "status")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Docker is not available")
	assert.Contains(t, stdout, "Install Docker:")
}

func TestLogsShowsDockerErrorWhenUnhealthy(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", unhealthyDockerEnv(), "logs")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Docker is not available")
	assert.Contains(t, stdout, "Install Docker:")
}
