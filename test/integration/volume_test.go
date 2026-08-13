package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumePathCommand(t *testing.T) {
	t.Parallel()
	t.Run("prints default volume path", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
		customVolume := filepath.Join(t.TempDir(), "my-volume")
		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(customVolume) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertSamePath(t, customVolume, stdout)
	})

	t.Run("follows volumes entry targeting the persistence path", func(t *testing.T) {
		t.Parallel()
		persistDir := filepath.Join(t.TempDir(), "persist")
		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volumes = ["` + escapeTomlPath(persistDir) + `:/var/lib/localstack", "/abs/init.sf.sql:/etc/localstack/init/ready.d/init.sf.sql"]
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertSamePath(t, persistDir, stdout)
	})

	t.Run("resolves a relative persistence source against the config directory", func(t *testing.T) {
		t.Parallel()
		configDir := t.TempDir()
		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volumes = ["./persist:/var/lib/localstack"]
`
		configFile := filepath.Join(configDir, "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "volume", "path")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		assertSamePath(t, filepath.Join(configDir, "persist"), stdout)
	})

	t.Run("emits telemetry", func(t *testing.T) {
		t.Parallel()
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
	t.Parallel()
	t.Run("clears volume with force flag", func(t *testing.T) {
		t.Parallel()
		volumeDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(volumeDir, "cache", "certs"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "cache", "certs", "cert.pem"), []byte("fake cert"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "cache", "machine.json"), []byte("{}"), 0644))

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(volumeDir) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "--non-interactive", "volume", "clear", "--force")
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
		t.Parallel()
		tmpHome := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		configFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		writeConfigFile(t, configFile)

		e := testEnvWithHome(tmpHome, xdgOverride)
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "--non-interactive", "volume", "clear")
		require.Error(t, err)
		requireExitCode(t, 1, err)

		snap.Match(t, sanitizeOutput(stderr))
	})

	t.Run("handles nonexistent volume directory", func(t *testing.T) {
		t.Parallel()
		volumeDir := filepath.Join(t.TempDir(), "does-not-exist")

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(volumeDir) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "--non-interactive", "volume", "clear", "--force")
		require.NoError(t, err, "lstk volume clear failed: %s\nstdout: %s", stderr, stdout)
		requireExitCode(t, 0, err)

		assert.Contains(t, stdout, "Volume data cleared")
	})

	t.Run("filters by emulator type", func(t *testing.T) {
		t.Parallel()
		volumeDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "data.json"), []byte("{}"), 0644))

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(volumeDir) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		// Wrong type should fail
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "--non-interactive", "volume", "clear", "--force", "--type", "snowflake")
		require.Error(t, err)
		requireExitCode(t, 1, err)
		snap.Match(t, sanitizeOutput(stderr))

		// Correct type should succeed
		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "--non-interactive", "volume", "clear", "--force", "--type", "aws")
		require.NoError(t, err, "lstk volume clear failed: %s\nstdout: %s", stderr, stdout)
		requireExitCode(t, 0, err)

		entries, err := os.ReadDir(volumeDir)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("emits telemetry", func(t *testing.T) {
		t.Parallel()
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

	t.Run("suggests sudo when volume contains root-owned files", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS != "linux" {
			t.Skip("root-owned bind-mount files only occur on Linux")
		}
		if os.Getuid() == 0 {
			t.Skip("test requires non-root user")
		}

		volumeDir := t.TempDir()

		// Simulate LocalStack creating files as root inside a bind-mounted volume.
		out, err := exec.CommandContext(testContext(t), "docker", "run", "--rm",
			"-v", volumeDir+":/vol",
			"alpine", "sh", "-c", "mkdir /vol/cache && touch /vol/cache/cert.pem",
		).CombinedOutput()
		require.NoError(t, err, "docker setup failed: %s", out)
		t.Cleanup(func() {
			_ = exec.Command("docker", "run", "--rm",
				"-v", volumeDir+":/vol",
				"alpine", "sh", "-c", "rm -rf /vol/cache",
			).Run()
		})

		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(volumeDir) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "--non-interactive", "volume", "clear", "--force")
		if err == nil {
			t.Skip("Docker is configured with user namespace remapping; root-owned files cleared without issue")
		}

		requireExitCode(t, 1, err)
		assert.Contains(t, stderr, "sudo")
	})
}

func TestVolumeClearInteractive(t *testing.T) {
	t.Parallel()

	makeConfig := func(t *testing.T, volumeDir string) string {
		t.Helper()
		configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
volume = "` + escapeTomlPath(volumeDir) + `"
`
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))
		return configFile
	}

	// startVolumeClear launches lstk volume clear in a PTY and waits for the
	// confirmation prompt.
	startVolumeClear := func(t *testing.T, configFile string) *ptyProc {
		t.Helper()
		p := startLstkInPTY(t, testContext(t), testEnvWithHome(t.TempDir(), ""), "--config", configFile, "volume", "clear")
		// An irreversible confirmation stays inline and capitalizes the answer
		// ENTER picks, so a stray ENTER cannot wipe the volume.
		p.waitForOutput("Clear volume data? This cannot be undone [y/N]", "confirmation prompt should appear")
		return p
	}

	t.Run("clears volume when user confirms with y", func(t *testing.T) {
		t.Parallel()
		volumeDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "data.json"), []byte("{}"), 0644))

		p := startVolumeClear(t, makeConfig(t, volumeDir))
		p.write("y")
		out, err := p.wait()
		require.NoError(t, err)

		assert.Contains(t, out, "Volume data cleared")
		entries, err := os.ReadDir(volumeDir)
		require.NoError(t, err)
		assert.Empty(t, entries, "volume directory should be empty after confirm")
	})

	t.Run("cancels when user presses n", func(t *testing.T) {
		t.Parallel()
		volumeDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(volumeDir, "data.json"), []byte("{}"), 0644))

		p := startVolumeClear(t, makeConfig(t, volumeDir))
		p.write("n")
		out, err := p.wait()
		require.NoError(t, err)

		assert.Contains(t, out, "Cancelled")
		entries, err := os.ReadDir(volumeDir)
		require.NoError(t, err)
		assert.Len(t, entries, 1, "volume directory should be untouched after cancel")
	})
}

// escapeTomlPath escapes backslashes for Windows paths in TOML quoted strings.
func escapeTomlPath(path string) string {
	return strings.ReplaceAll(path, `\`, `\\`)
}
