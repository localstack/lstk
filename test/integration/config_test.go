package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFileCreatedOnStartup(t *testing.T) {
	tmpHome := t.TempDir()

	var configDir string
	var env []string

	switch runtime.GOOS {
	case "darwin":
		configDir = filepath.Join(tmpHome, "Library", "Application Support", "lstk")
		env = append(os.Environ(), "HOME="+tmpHome)
	case "linux":
		configDir = filepath.Join(tmpHome, ".config", "lstk")
		env = append(os.Environ(), "HOME="+tmpHome, "XDG_CONFIG_HOME=")
	case "windows":
		configDir = filepath.Join(tmpHome, "AppData", "Roaming", "lstk")
		env = append(os.Environ(), "APPDATA="+filepath.Join(tmpHome, "AppData", "Roaming"))
	default:
		t.Skipf("unsupported OS: %s", runtime.GOOS)
	}

	configFile := filepath.Join(configDir, "config.toml")

	cmd := exec.Command("../../bin/lstk", "start")
	cmd.Env = env
	cmd.Start()
	defer cmd.Process.Kill()

	// Poll for config file creation - check every 50ms, timeout after 2s
	require.Eventually(t, func() bool {
		_, err := os.Stat(configFile)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond, "config.toml should be created")

	assert.DirExists(t, configDir, "config directory should be created at OS-specific location")

	content, err := os.ReadFile(configFile)
	require.NoError(t, err)

	configStr := string(content)
	assert.Contains(t, configStr, `type = 'aws'`)
	assert.Contains(t, configStr, `tag = 'latest'`)
	assert.Contains(t, configStr, `port = '4566'`)
	assert.Contains(t, configStr, `health_path = '/_localstack/health'`)
}
