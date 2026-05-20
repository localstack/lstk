package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// azureSetupEnv returns a base environment with an isolated HOME so that tests
// never read or write the developer's real Azure/AWS config files.
func azureSetupEnv(t *testing.T) (env.Environ, string) {
	t.Helper()
	tmpHome := t.TempDir()
	return env.With(env.Home, tmpHome), tmpHome
}

func TestSetupAzureNonInteractiveReturnsError(t *testing.T) {
	t.Parallel()
	baseEnv, _ := azureSetupEnv(t)

	_, stderr, err := runLstk(t, testContext(t), "",
		baseEnv,
		"setup", "azure",
	)
	require.Error(t, err)
	assert.Contains(t, stderr, "setup azure requires an interactive terminal")
}

// writeConfigToml writes the given TOML content to $HOME/.config/lstk/config.toml
// under the temp home so lstk picks it up via its standard config resolution.
func writeConfigToml(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".config", "lstk")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0600))
}

func TestSetupAzureErrorsWhenNoAzureEmulatorConfigured(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := azureSetupEnv(t)

	// Config that only declares an AWS emulator — no [[containers]] of type "azure".
	writeConfigToml(t, tmpHome, `
[[containers]]
type = "aws"
port = "4566"
`)

	out, err := runLstkInPTY(t, testContext(t), baseEnv, "setup", "azure")
	require.Error(t, err, "expected setup azure to fail without an azure container in config")
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "no azure emulator configured")
}
