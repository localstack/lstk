package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		_, stderr, err := runLstk(t, workDir, e, "logout")
		require.NoError(t, err, stderr)

		expectedConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
		assert.FileExists(t, expectedConfigFile)
		assertDefaultConfigContent(t, expectedConfigFile)
	})

	t.Run("falls back to os user config dir when home .config is missing", func(t *testing.T) {
		tmpHome := t.TempDir()
		workDir := t.TempDir()
		xdgOverride := filepath.Join(tmpHome, "xdg-config-home")

		e := testEnvWithHome(tmpHome, xdgOverride)
		_, stderr, err := runLstk(t, workDir, e, "logout")
		require.NoError(t, err, stderr)

		expectedConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")
		assert.FileExists(t, expectedConfigFile)
		assertDefaultConfigContent(t, expectedConfigFile)
	})
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
	stdout, stderr, err := runLstk(t, workDir, e, "config", "path")
	require.NoError(t, err, stderr)

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
	stdout, stderr, err := runLstk(t, workDir, e, "config", "path")
	require.NoError(t, err, stderr)

	assertSamePath(t, xdgConfigFile, stdout)
}

func TestConfigPathCommand(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgConfigFile := filepath.Join(tmpHome, ".config", "lstk", "config.toml")
	writeConfigFile(t, xdgConfigFile)

	e := testEnvWithHome(tmpHome, filepath.Join(tmpHome, "xdg-config-home"))
	stdout, stderr, err := runLstk(t, workDir, e, "config", "path")
	require.NoError(t, err, stderr)

	assertSamePath(t, xdgConfigFile, stdout)
}

func TestConfigPathCommandDoesNotCreateConfig(t *testing.T) {
	tmpHome := t.TempDir()
	workDir := t.TempDir()
	xdgOverride := filepath.Join(tmpHome, "xdg-config-home")
	expectedConfigFile := filepath.Join(expectedOSConfigDir(tmpHome, xdgOverride), "config.toml")

	e := testEnvWithHome(tmpHome, xdgOverride)
	stdout, stderr, err := runLstk(t, workDir, e, "config", "path")
	require.NoError(t, err, stderr)

	assertSamePath(t, expectedConfigFile, stdout)
	assert.NoFileExists(t, expectedConfigFile)
}

func runLstk(t *testing.T, dir string, env []string, args ...string) (string, string, error) {
	t.Helper()

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.Command(binPath, args...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
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
