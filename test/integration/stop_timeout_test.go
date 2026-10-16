package integration_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shutdownTimeoutOverride env.Key = "LOCALSTACK_SHUTDOWN_TIMEOUT"

// The container lstk starts carries a Docker stop timeout of the emulator's own SHUTDOWN_TIMEOUT plus a margin, so
// `lstk stop` and `docker stop` wait for the emulator's shutdown instead of killing it after Docker's 10 s default.
// A LOCALSTACK_SHUTDOWN_TIMEOUT from the host wins over the profile's SHUTDOWN_TIMEOUT, as the image's entrypoint
// re-exports it. The stand-in nginx image never becomes healthy, so the start times out and leaves the container to
// inspect.
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

	e := env.Environ(testEnvWithHome(home, "")).
		Without(shutdownTimeoutOverride).
		With(shutdownTimeoutOverride, "30").
		With(env.APIEndpoint, mockServer.URL).
		With(env.AuthToken, "fake-token").
		With(env.StartupTimeout, "5s")
	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "did not become ready within 5s")

	inspect, err := dockerClient.ContainerInspect(ctx, wantContainer, client.ContainerInspectOptions{})
	require.NoError(t, err, "the container must stay after the startup timeout")
	require.NotNil(t, inspect.Container.Config.StopTimeout, "the container must carry its own stop timeout")
	assert.Equal(t, 35, *inspect.Container.Config.StopTimeout, "LOCALSTACK_SHUTDOWN_TIMEOUT (30 s) plus the 5 s margin")
}

// `lstk stop` lets an emulator lstk started finish a shutdown longer than Docker's 10 s default: the stand-in takes
// 12 s to exit after its stop signal and sets SHUTDOWN_TIMEOUT=15 in its image, so it exits 0 instead of being
// killed (137).
func TestStopWaitsForTheEmulatorsOwnShutdown(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const imageName = "lstk-slow-shutdown"
	const imageTag = "slow-shutdown-test"
	commitSlowShutdownImage(t, ctx, imageName+":"+imageTag)

	// A non-interactive startup timeout leaves the container running as localstack-aws-<tag>, which cleanup() does
	// not remove.
	wantContainer := "localstack-aws-" + imageTag
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), wantContainer, client.ContainerRemoveOptions{Force: true})
	})

	home := t.TempDir()
	configFile := filepath.Join(home, "config.toml")
	require.NoError(t, os.WriteFile(configFile,
		[]byte(fmt.Sprintf("[[containers]]\ntype = \"aws\"\nport = \"4566\"\nimage = %q\ntag = %q\n", imageName, imageTag)), 0644))

	// A pinned tag already present locally skips both the pull and the license checks, so a dummy token is enough.
	e := env.Environ(testEnvWithHome(home, "")).Without(shutdownTimeoutOverride).With(env.AuthToken, "fake-token")
	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start", "--timeout", "3s")
	requireExitCode(t, 1, err)
	require.Contains(t, stdout+stderr, "did not become ready within 3s")

	exit := awaitNextExit(t, ctx, wantContainer)
	_, stderr, err = runLstk(t, ctx, "", e, "--config", configFile, "stop")
	require.NoError(t, err, "lstk stop failed: %s", stderr)

	requireCleanExit(t, exit)
}

// `lstk stop` also waits for the shutdown of an emulator it did not start, and so carries no stop timeout of its own
// (started outside lstk, or by an older lstk): it takes the timeout from the container's SHUTDOWN_TIMEOUT.
func TestStopWaitsForTheShutdownOfAnEmulatorStartedElsewhere(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const image = "localstack/localstack-pro:slow-shutdown-test"
	commitSlowShutdownImage(t, ctx, image)

	const name = "localstack-external-slow-shutdown"
	containerPort := network.MustParsePort("4566/tcp")
	resp, err := dockerClient.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        image,
			ExposedPorts: network.PortSet{containerPort: struct{}{}},
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{containerPort: []network.PortBinding{{}}},
		},
		Name: name,
	})
	require.NoError(t, err, "failed to create the external container")
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), name, client.ContainerRemoveOptions{Force: true})
	})
	_, err = dockerClient.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	require.NoError(t, err, "failed to start the external container")

	exit := awaitNextExit(t, ctx, name)
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).Without(shutdownTimeoutOverride)
	_, stderr, err := runLstk(t, ctx, "", e, "stop")
	require.NoError(t, err, "lstk stop failed: %s", stderr)

	requireCleanExit(t, exit)
}

// commitSlowShutdownImage builds a local-only image that stays running, never serves /_localstack/health, sets
// SHUTDOWN_TIMEOUT=15 and exits 0 twelve seconds after a stop signal: longer than Docker's 10 s default stop timeout,
// shorter than the 20 s lstk gives it.
func commitSlowShutdownImage(t *testing.T, ctx context.Context, imageRef string) {
	t.Helper()
	commitTestImage(t, ctx, imageRef, "lstk-slow-shutdown-src",
		"ENV SHUTDOWN_TIMEOUT=15",
		`CMD ["sh", "-c", "trap 'sleep 12; exit 0' TERM; while :; do sleep 1; done"]`,
	)
}

// containerExit is how a container's exit wait ended: its exit code, or why the wait failed.
type containerExit struct {
	code int64
	err  error
}

// awaitNextExit registers a wait for the named container's next exit, before anything stops it, and returns a channel
// that receives how it ended. With AutoRemove the container is gone right after it exits, so the wait must exist
// first; the wait ends with the test context at the latest.
func awaitNextExit(t *testing.T, ctx context.Context, name string) <-chan containerExit {
	t.Helper()
	wait := dockerClient.ContainerWait(ctx, name, client.ContainerWaitOptions{Condition: container.WaitConditionNextExit})
	exit := make(chan containerExit, 1)
	go func() {
		select {
		case result := <-wait.Result:
			exit <- containerExit{code: result.StatusCode}
		case err := <-wait.Error:
			exit <- containerExit{err: err}
		}
	}()
	return exit
}

// requireCleanExit waits for the exit awaitNextExit registered and requires exit code 0: the emulator finished its
// shutdown, where Docker's kill would have left 137.
func requireCleanExit(t *testing.T, exit <-chan containerExit) {
	t.Helper()
	outcome := <-exit
	require.NoError(t, outcome.err, "waiting for the container to exit")
	assert.Equal(t, int64(0), outcome.code, "the emulator must finish its shutdown, not be killed by Docker")
}
