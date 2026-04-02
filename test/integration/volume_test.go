package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumePathCommand(t *testing.T) {
	t.Run("prints default volume path", func(t *testing.T) {
		tmpHome := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		writeConfigFile(t, configFile)

		e := testEnvWithHome(tmpHome, xdgOverride)
		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assert.Contains(t, stdout, filepath.Join("lstk", "volume", "localstack-aws"))
	})

	t.Run("prints custom volume path from config", func(t *testing.T) {
		customVolume := filepath.Join(t.TempDir(), "my-volume")
		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + customVolume + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertSamePath(t, customVolume, stdout)
	})

	t.Run("emits telemetry", func(t *testing.T) {
		tmpHome := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		writeConfigFile(t, configFile)

		analyticsSrv, events := mockAnalyticsServer(t)
		e := env.Environ(testEnvWithHome(tmpHome, xdgOverride)).With(env.AnalyticsEndpoint, analyticsSrv.URL)
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertCommandTelemetry(t, events, "volume path", 0)
	})
}
