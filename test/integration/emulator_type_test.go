package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dockerHostKey points the binary at a non-existent Docker socket so that
// `start` fails fast right after applying the --type flag, letting these tests
// exercise the config mutation and messaging without needing a Docker daemon.
const dockerHostKey = env.Key("DOCKER_HOST")

// typeTestEnv builds an isolated environment whose start path fails at the Docker
// ping, so the emulator-type handling runs but nothing is actually started.
func typeTestEnv(t *testing.T) (env.Environ, string) {
	t.Helper()
	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1").
		With(env.AuthToken, "dummy-token").
		With(dockerHostKey, "unix:///nonexistent-lstk-test.sock")
	return e, tmpHome
}

// typeTestEnvWithDocker is like typeTestEnv but leaves DOCKER_HOST untouched, so
// the binary talks to the real Docker daemon — needed by tests that must detect
// an actually-running container rather than failing fast at the Docker ping.
func typeTestEnvWithDocker(t *testing.T) (env.Environ, string) {
	t.Helper()
	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1").
		With(env.AuthToken, "fake-token")
	return e, tmpHome
}

func resolvedConfigPath(t *testing.T, e env.Environ) string {
	t.Helper()
	configPath, _, err := runLstk(t, testContext(t), t.TempDir(), e, "config", "path")
	require.NoError(t, err)
	return configPath
}

func TestStartTypeFlagFirstRunCreatesConfig(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoFileExists(t, configPath)

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "snowflake", "--non-interactive")

	assert.Contains(t, stdout, "Snowflake emulator selected.")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "snowflake"`)
}

// TestBareRootTypeFlagCreatesConfig covers the bare-root form (no "start"
// subcommand) documented in the README/CLAUDE.md; it exercises the
// non-interspersed flag parsing that routes a leading positional to extension
// dispatch, so only the flag form is valid on the root.
func TestBareRootTypeFlagCreatesConfig(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoFileExists(t, configPath)

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "--type", "azure", "--non-interactive")

	assert.Contains(t, stdout, "Azure emulator selected.")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "azure"`)
}

func TestStartTypeFlagSwitchesInPlace(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"aws\"     # keep me\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "azure", "--non-interactive")

	assert.Contains(t, stdout, "Switched configured emulator to Azure")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "azure"`)
	// The surgical rewrite preserves the inline comment and other fields.
	assert.Contains(t, string(data), "# keep me")
	assert.Contains(t, string(data), `port = "4566"`)
}

func TestStartTypeFlagNoOpWhenMatching(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	stdout, _, _ := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "aws", "--non-interactive")

	assert.NotContains(t, stdout, "Switched configured emulator")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestStartTypeFlagErrorsWhenImageSet(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\nimage = \"my-registry.example.com/localstack-pro:3.0\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "snowflake", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stdout, "Cannot switch emulator to Snowflake while a custom image is set")
	// Config must be left untouched.
	data, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestStartTypeErrorsOnMultipleBlocks verifies the switch refuses a config with
// more than one [[containers]] block before mutating it, so neither block's type
// is rewritten by a start that cannot succeed anyway.
func TestStartTypeErrorsOnMultipleBlocks(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4567\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "azure", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stdout, "Unsupported configuration")
	// Config must be left untouched — neither block's type is rewritten.
	data, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestStartTypePositionalRejected pins that the emulator is a flag only: a
// positional (`lstk start azure`) is rejected with a hint pointing at --type,
// rather than silently starting AWS (the pre-fix behavior) or being accepted as
// an alias. This keeps one spelling and avoids colliding with the `aws`/`az`
// proxy subcommands that a positional mental model would imply on the root.
func TestStartTypePositionalRejected(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "azure", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stderr, "select the emulator with --type")
	require.NoFileExists(t, configPath)
}

// TestStartTypeErrorsWhenNoContainersBlock is the end-to-end regression for the
// block-scoped rewrite: a config file with a `type` key in an [env.*] table but
// no [[containers]] block must fail with a clear error rather than silently
// rewriting the unrelated env key (the pre-fix behavior of an unscoped match).
func TestStartTypeErrorsWhenNoContainersBlock(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	content := "[env.default]\ntype = \"custom\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "azure", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stderr, "[[containers]] block")
	// The env table's type key must be left untouched, not corrupted to "azure".
	data, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

func TestStartTypeInvalidValue(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "bogus", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stderr, `invalid emulator type "bogus"`)
}

// TestStartTypeFlagRefusesSwitchWhenDifferentEmulatorRunning is the end-to-end
// regression for the reviewer-reported bug: `lstk -t azure` followed by `lstk -t
// snowflake` while Azure was still running rewrote the config to Snowflake even
// though the start itself failed on the port conflict, leaving `status`/`stop`/
// `logs` unable to find the still-running Azure emulator (they resolve from the
// configured type). The switch must be refused — and the config left untouched
// — when a different emulator is already running on the port the requested type
// would use. Uses a real Docker daemon (not typeTestEnv's fake DOCKER_HOST)
// since the conflict can only be detected by actually finding the running
// container; alpine (already used elsewhere in this suite) is tagged as a fake
// Azure image so no real product image needs to be pulled.
func TestStartTypeFlagRefusesSwitchWhenDifferentEmulatorRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-azure:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})
	startExternalContainer(t, ctx, fakeImage, "localstack-external-azure", "4566")

	e, _ := typeTestEnvWithDocker(t)
	configPath := resolvedConfigPath(t, e)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	content := "[[containers]]\ntype = \"azure\"\ntag = \"latest\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "start", "--type", "snowflake", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stdout, "LocalStack Azure Emulator is running on port 4566")
	assert.Contains(t, stdout, "config was not changed")
	assert.Contains(t, stdout, "docker stop localstack-external-azure")

	// Config must be left untouched: still Azure, not rewritten to Snowflake.
	data, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}
