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
