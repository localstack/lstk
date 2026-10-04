package integration_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestStartImageFlagRejectsInvalidReference(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--image", "bad image;rm", "--non-interactive")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "invalid image")
	assert.NoFileExists(t, configPath, "a rejected --image must not create a config")
}

func TestStartImageFlagRejectsRegistryPrefixedImageOfAnotherType(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "--non-interactive", "start", "--image", "docker.io/localstack/snowflake:latest")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "docker.io/localstack/snowflake:latest is a Snowflake emulator image")
}

func TestStartImageFlagFirstRunRejectsKnownImageOfAnotherType(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoFileExists(t, configPath)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"start", "--image", "localstack/snowflake", "--non-interactive")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "localstack/snowflake is a Snowflake emulator image")
	assert.NoFileExists(t, configPath, "a rejected --image must not create a config")
}

func TestStartImageFlagRejectsKnownImageOfAnotherType(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	const content = "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n"
	configFile := writeTestConfig(t, content)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "--non-interactive", "-t", "aws", "--image", "localstack/snowflake")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "localstack/snowflake is a Snowflake emulator image")
	data, readErr := os.ReadFile(configFile)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data), "the config must not change when --image is rejected")
}

func TestStartImageFlagKnowsSnowflakeNextIsSnowflake(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "--non-interactive", "--image", "localstack/snowflake-next")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "localstack/snowflake-next is a Snowflake emulator image")
	assert.Contains(t, stdout+stderr, "lstk --type snowflake --image localstack/snowflake-next")
}

func TestRestartImageFlagRejectsKnownImageOfAnotherType(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "--non-interactive", "restart", "--image", "localstack/snowflake")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "localstack/snowflake is a Snowflake emulator image")
}

func TestStartImageFlagFirstRunDoesNotWriteImageToConfig(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoFileExists(t, configPath)

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e,
		"-t", "snowflake", "--image", "localstack/snowflake-next", "--non-interactive")

	assert.Contains(t, stdout, "Snowflake emulator selected.")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "snowflake"`)
	assert.NotContains(t, string(data), "snowflake-next")
	assert.NotRegexp(t, `(?m)^\s*image\s*=`, string(data))
}

func TestStartImageFlagWithTypeSwitchDoesNotWriteImageToConfig(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n")

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e,
		"--config", configFile, "--non-interactive", "-t", "snowflake", "--image", "localstack/snowflake-next")

	assert.Contains(t, stdout, "Switched configured emulator to Snowflake")
	data, err := os.ReadFile(configFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "snowflake"`)
	assert.NotContains(t, string(data), "snowflake-next")
}

func TestStartImageFlagOverridesConfiguredImage(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	const content = "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\nimage = \"lstk-configured-image\"\n"
	configFile := writeTestConfig(t, content)

	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "dummy-token")
	stdout, stderr, err := runLstk(t, testContext(t), "", e,
		"--config", configFile, "--non-interactive", "start", "--image", "lstk-nonexistent-flag-image")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, "Failed to pull lstk-nonexistent-flag-image:latest")
	assert.NotContains(t, stdout+stderr, "lstk-configured-image")
	data, readErr := os.ReadFile(configFile)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data), "--image must never be written to the config")
}

// A pinned config tag must not suppress the floating-tag checks on --image.
// Otherwise a locally present :latest image is started with no pull and no
// "does this look like an emulator" inspection.
func TestStartImageFlagPinnedConfigTagStillInspectsFloatingImage(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	const localImage = "lstk-floating-image-flag"
	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: localImage + ":latest"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), localImage+":latest", client.ImageRemoveOptions{Force: true})
	})
	// A pinned config tag names the container localstack-aws-<tag>. cleanup()
	// only removes the bare localstack-aws name.
	t.Cleanup(func() {
		_, _ = dockerClient.ContainerRemove(context.Background(), "localstack-aws-2026.4", client.ContainerRemoveOptions{Force: true})
	})

	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"2026.4\"\nport = \"4566\"\n")
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "dummy-token")
	stdout, stderr, err := runLstk(t, ctx, "", e,
		"--config", configFile, "--non-interactive", "start", "--image", localImage+":latest", "--timeout", "3s")

	require.Error(t, err)
	requireExitCode(t, 1, err)
	out := stdout + stderr
	assert.Contains(t, out, localImage+":latest does not look like a LocalStack AWS emulator image")
	assert.NotContains(t, out, "Using local image "+localImage+":latest")
}

func TestStartExplainsImageThatIsNotAnAWSEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	const localImage = "lstk-not-an-emulator"
	reader, err := dockerClient.ImagePull(ctx, testImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull test image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: localImage + ":latest"})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), localImage+":latest", client.ImageRemoveOptions{})
	})

	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n")
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "dummy-token")
	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start", "--image", localImage)

	require.Error(t, err)
	requireExitCode(t, 1, err)
	out := stdout + stderr
	assert.Contains(t, out, "lstk-not-an-emulator:latest does not look like a LocalStack AWS emulator image")
	assert.Contains(t, out, "lstk --type <snowflake|azure> --image lstk-not-an-emulator:latest")
	assert.NotContains(t, out, "LOCALSTACK_BUILD_VERSION")
}

func TestRestartKeepsImageFromImageFlag(t *testing.T) {
	requireDocker(t)
	authToken := env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	const sourceImage = "localstack/localstack-pro:latest"
	const localImage = "lstk-image-flag-test"
	reader, err := dockerClient.ImagePull(ctx, sourceImage, client.ImagePullOptions{})
	require.NoError(t, err, "failed to pull source image")
	_, _ = io.Copy(io.Discard, reader)
	_ = reader.Close()
	_, err = dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: sourceImage, Target: localImage + ":latest"})
	require.NoError(t, err, "failed to tag local image")
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), localImage+":latest", client.ImageRemoveOptions{})
	})

	home := t.TempDir()
	scheduleVolumeCleanup(t, home)
	configFile := writeTestConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n")

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()
	e := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, mockServer.URL).
		With(env.AuthToken, authToken)

	_, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start", "--image", localImage)
	require.NoError(t, err, "lstk start --image failed: %s", stderr)
	requireContainerImage(t, ctx, localImage+":latest")

	stdout, stderr, err := runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "start", "--image", "lstk-other-image")
	require.NoError(t, err, "second start should report the running emulator: %s", stderr)
	assert.Contains(t, stdout+stderr, "--image lstk-other-image:latest was not applied")

	_, stderr, err = runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "restart")
	require.NoError(t, err, "lstk restart failed: %s", stderr)
	requireContainerImage(t, ctx, localImage+":latest")

	_, stderr, err = runLstk(t, ctx, "", e, "--config", configFile, "--non-interactive", "stop")
	require.NoError(t, err, "lstk stop should find the container started with --image: %s", stderr)
}

func requireContainerImage(t *testing.T, ctx context.Context, want string) {
	t.Helper()
	inspect, err := dockerClient.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})
	require.NoError(t, err, "failed to inspect container")
	require.True(t, inspect.Container.State.Running, "container should be running")
	assert.Equal(t, want, inspect.Container.Config.Image)
}
