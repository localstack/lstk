package integration_test

import (
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowsDockerErrorEnv returns an Environ with an invalid DOCKER_HOST and no PSModulePath.
func windowsDockerErrorEnv() env.Environ {
	return env.Without(env.Key("PSModulePath"), env.Key("DOCKER_HOST")).
		With(env.AuthToken, "fake-token").
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1")
}

// Verifies that when docker is in PATH, lstk suggests "docker desktop start".
func TestWindowsDockerErrorShowsDockerCLICommand(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", windowsDockerErrorEnv(), "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "docker desktop start")
}

// Verifies that the verbose Docker error message is suppressed on Windows.
func TestWindowsDockerErrorOmitsVerboseSummary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	stdout, _, err := runLstk(t, testContext(t), "", windowsDockerErrorEnv(), "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Docker is not available")
	assert.NotContains(t, stdout, "cannot connect to Docker daemon")
}
