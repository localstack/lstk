package integration_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The container lstk starts carries a Docker stop timeout of the emulator's own SHUTDOWN_TIMEOUT plus a margin, so
// `lstk stop` and `docker stop` wait for the emulator's shutdown instead of killing it after Docker's 10 s default.
// The stand-in nginx image never becomes healthy, so the start times out and leaves the container to inspect.
func TestStartGivesTheContainerItsShutdownTimeoutToStop(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const standInImage = "nginx:alpine"
	const pinnedTag = "stop-timeout-test"
	const pinnedImage = "localstack/localstack-pro:" + pinnedTag
	reader, err := dockerClient.ImagePull(ctx, standInImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull nginx test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: standInImage, Target: pinnedImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), pinnedImage, client.ImageRemoveOptions{})
	})

	wantContainer := "localstack-aws-" + pinnedTag
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), wantContainer, client.ContainerRemoveOptions{Force: true})
	})

	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(fmt.Sprintf(
		"[[containers]]\ntype = \"aws\"\ntag = %q\nport = \"4598\"\nenv = [\"shutdown\"]\n\n[env.shutdown]\nSHUTDOWN_TIMEOUT = \"20\"\n",
		pinnedTag,
	)), 0644))

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e := append(testEnvWithHome(home, ""),
		string(env.APIEndpoint)+"="+mockServer.URL,
		string(env.AuthToken)+"=fake-token",
		string(env.StartupTimeout)+"=5s")
	_, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start")
	require.Error(t, err, "the stand-in never becomes healthy: %s", stderr)

	inspect, err := dockerClient.ContainerInspect(ctx, wantContainer, client.ContainerInspectOptions{})
	require.NoError(t, err, "the container must stay after the startup timeout")
	require.NotNil(t, inspect.Container.Config.StopTimeout, "the container must carry its own stop timeout")
	assert.Equal(t, 25, *inspect.Container.Config.StopTimeout, "SHUTDOWN_TIMEOUT (20 s) plus the 5 s margin")
}
