package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
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

func TestStartTypeInvalidValue(t *testing.T) {
	t.Parallel()
	e, _ := typeTestEnv(t)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "start", "--type", "bogus", "--non-interactive")

	require.Error(t, err)
	assert.Contains(t, stderr, `invalid emulator type "bogus"`)
}
