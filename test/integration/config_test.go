package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFileCreatedOnStartup(t *testing.T) {
	t.Run("creates in home .config when present", func(t *testing.T) {
		tmpHome := t.TempDir()
		workDir := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
		require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))

		e := testEnvWithHome(tmpHome, xdgOverride)
		_, stderr, err := runLstk(t, testContext(t), workDir, e, "logout")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		expectedConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		assert.FileExists(t, expectedConfigFile)
		assertDefaultConfigContent(t, expectedConfigFile)
	})

	t.Run("falls back to os user config dir when home .config is missing", func(t *testing.T) {
		tmpHome := t.TempDir()
		workDir := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")

		e := testEnvWithHome(tmpHome, xdgOverride)
		_, stderr, err := runLstk(t, testContext(t), workDir, e, "logout")
		require.NoError(t, err, stderr)
		requireExitCode(t, 0, err)

		expectedConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")
		assert.FileExists(t, expectedConfigFile)
		assertDefaultConfigContent(t, expectedConfigFile)
	})
}

func TestConfigFlagEnvVarsPassedToContainer(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4566"
env = ["test"]

[env.test]
IAM_SOFT_MODE = "1"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL), "--config", configFile, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)
	requireExitCode(t, 0, err)

	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	require.NoError(t, err, "failed to inspect container")
	assert.Contains(t, inspect.Config.Env, "IAM_SOFT_MODE=1")
}

func TestConfigFlagOverridesConfigPath(t *testing.T) {
	customConfig := filepath.Join(t.TempDir(), "custom.toml")
	writeConfigFile(t, customConfig)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", customConfig, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assertSamePath(t, customConfig, stdout)
}

func TestLocalConfigTakesPrecedence(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgOverride := filepath.Join(tmpHome, "xdg-config-home")

	localConfigFile := filepath.Join(workDir, "lstk.toml")
	writeConfigFile(t, localConfigFile)
	writeConfigFile(t, filepath.Join(tmpHome, ".config", "lstk", "config.toml"))
	writeConfigFile(t, filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml"))

	e := testEnvWithHome(tmpHome, xdgOverride)
	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	expectedLocalPath, err := filepath.Abs(localConfigFile)
	require.NoError(t, err)
	assertSamePath(t, expectedLocalPath, stdout)
}

func TestXDGConfigTakesPrecedence(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgOverride := filepath.Join(tmpHome, "xdg-config-home")

	xdgConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
	osConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")
	writeConfigFile(t, xdgConfigFile)
	writeConfigFile(t, osConfigFile)

	e := testEnvWithHome(tmpHome, xdgOverride)
	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assertSamePath(t, xdgConfigFile, stdout)
}

func TestConfigPathCommand(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
	writeConfigFile(t, xdgConfigFile)

	analyticsSrv, events := mockAnalyticsServer(t)
	e := env.Environ(testEnvWithHome(tmpHome, filepath.Join(tmpHome, "xdg-config-home"))).With(env.AnalyticsEndpoint, analyticsSrv.URL)
	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assertSamePath(t, xdgConfigFile, stdout)
	assertCommandTelemetry(t, events, "config path", 0)
}

func TestConfigPathCommandDoesNotCreateConfig(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
	expectedConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")

	e := testEnvWithHome(tmpHome, xdgOverride)
	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assertSamePath(t, expectedConfigFile, stdout)
	assert.NoFileExists(t, expectedConfigFile)
}

func TestConfigWithUnknownFieldsIsAccepted(t *testing.T) {
	configContent := `
unknown_top_level = "should be ignored"

[[containers]]
type = "aws"
tag = "latest"
port = "4566"
future_field = "should be ignored"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)
}

func TestConfigWithMissingRequiredPortFails(t *testing.T) {
	configContent := `
[[containers]]
type = "aws"
tag = "latest"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "stop")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "port is required")
}

func TestConfigWithMissingOptionalTagSucceeds(t *testing.T) {
	configContent := `
[[containers]]
type = "aws"
port = "4566"
`
	configFile := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--config", configFile, "config", "path")
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)
}

func testEnvWithHome(tmpHome, xdgConfigHome string) []string {
	e := env.Without("HOME", "XDG_CONFIG_HOME", "APPDATA", "USERPROFILE", "HOMEDRIVE", "HOMEPATH")
	switch runtime.GOOS {
	case "darwin", "linux":
		e = append(e, "HOME="+tmpHome, "XDG_CONFIG_HOME="+xdgConfigHome, fmt.Sprintf("%s=file", env.Keyring))
	case "windows":
		appData := filepath.Join(tmpHome, "AppData", "Roaming")
		e = append(e, "HOME="+tmpHome, "USERPROFILE="+tmpHome, "APPDATA="+appData, fmt.Sprintf("%s=file", env.Keyring))
	default:
		panic("unsupported OS: " + runtime.GOOS)
	}
	return e
}

func expectedOSConfigDir(tmpHome, xdgConfigHome string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(tmpHome, "Library", "Application Support", "lstk")
	case "linux":
		if xdgConfigHome != "" {
			return filepath.Join(xdgConfigHome, "lstk")
		}
		return filepath.Join(tmpHome, ".config", "lstk")
	case "windows":
		return filepath.Join(tmpHome, "AppData", "Roaming", "lstk")
	default:
		panic("unsupported OS: " + runtime.GOOS)
	}
}

func writeConfigFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func assertDefaultConfigContent(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	configStr := string(content)
	assert.Contains(t, configStr, "type")
	assert.Contains(t, configStr, "aws")
	assert.Contains(t, configStr, "tag")
	assert.Contains(t, configStr, "latest")
	assert.Contains(t, configStr, "port")
	assert.Contains(t, configStr, "4566")
}

func assertSamePath(t *testing.T, expectedPath, actualPath string) {
	t.Helper()
	assert.Equal(
		t,
		normalizedPath(expectedPath),
		normalizedPath(actualPath),
	)
}

func normalizedPath(path string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolvedPath)
	}
	return filepath.Clean(absPath)
}
