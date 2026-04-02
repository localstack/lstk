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

func TestVolumeClearCommand(t *testing.T) {
	t.Run("clears volume with force flag", func(t *testing.T) {
		volumeDir := t.TempDir()
		// Create some files in the volume directory
		require.NoError(t, os.MkdirAll(filepath.Join(volumeDir, "cache", "certs"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "cache", "certs", "cert.pem"), []byte("fake cert"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "cache", "machine.json"), []byte("{}"), 0644))

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + volumeDir + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "--non-interactive", "volume", "clear", "--force")
		require.NoError(t, err, "lstk volume clear failed: %s\nstdout: %s", stderr, stdout)
		requireExitCode(t, 0, err)

		assert.Contains(t, stdout, "Volume data cleared")

		// Directory itself should still exist
		_, err = os.Stat(volumeDir)
		require.NoError(t, err, "volume directory should still exist")

		// But contents should be gone
		entries, err := os.ReadDir(volumeDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "volume directory should be empty")
	})

	t.Run("fails without force in non-interactive mode", func(t *testing.T) {
		tmpHome := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		writeConfigFile(t, configFile)

		e := testEnvWithHome(tmpHome, xdgOverride)
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--non-interactive", "volume", "clear")
		require.Error(t, err)
		requireExitCode(t, 1, err)

		assert.Contains(t, stderr, "--force")
	})

	t.Run("handles nonexistent volume directory", func(t *testing.T) {
		volumeDir := filepath.Join(t.TempDir(), "does-not-exist")

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + volumeDir + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "--non-interactive", "volume", "clear", "--force")
		require.NoError(t, err, "lstk volume clear failed: %s\nstdout: %s", stderr, stdout)
		requireExitCode(t, 0, err)

		assert.Contains(t, stdout, "Volume data cleared")
	})

	t.Run("filters by container name", func(t *testing.T) {
		volumeDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "data.json"), []byte("{}"), 0644))

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + volumeDir + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		// Wrong container name should fail
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "--non-interactive", "volume", "clear", "--force", "--container", "localstack-snowflake")
		require.Error(t, err)
		requireExitCode(t, 1, err)
		assert.Contains(t, stderr, "not found")

		// Correct container name should succeed
		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "--non-interactive", "volume", "clear", "--force", "--container", "localstack-aws")
		require.NoError(t, err, "lstk volume clear failed: %s\nstdout: %s", stderr, stdout)
		requireExitCode(t, 0, err)

		entries, err := os.ReadDir(volumeDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("emits telemetry", func(t *testing.T) {
		tmpHome := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		writeConfigFile(t, configFile)

		analyticsSrv, events := mockAnalyticsServer(t)
		e := env.Environ(testEnvWithHome(tmpHome, xdgOverride)).With(env.AnalyticsEndpoint, analyticsSrv.URL)
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--non-interactive", "volume", "clear", "--force")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertCommandTelemetry(t, events, "volume clear", 0)
	})
}
