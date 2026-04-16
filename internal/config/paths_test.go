package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestFriendlyConfigPathRelativeForProjectLocal(t *testing.T) {
	// Cannot run in parallel: mutates process-wide cwd and viper state.

	tmpDir := t.TempDir()
	dir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	configDir := filepath.Join(dir, ".lstk")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	configFile := filepath.Join(configDir, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[aws]\n"), 0o644))

	origDir, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	friendly, err := FriendlyConfigPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".lstk", "config.toml"), friendly)
}

func TestFriendlyConfigPathTildeForHomeDir(t *testing.T) {
	// Cannot run in parallel: mutates process-wide viper state and HOME env.

	fakeHome := t.TempDir()
	resolvedHome, err := filepath.EvalSymlinks(fakeHome)
	require.NoError(t, err)

	configDir := filepath.Join(resolvedHome, ".config", "lstk")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	configFile := filepath.Join(configDir, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[aws]\n"), 0o644))

	t.Setenv("HOME", resolvedHome)

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	friendly, err := FriendlyConfigPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("~", ".config", "lstk", "config.toml"), friendly)
}

func TestLogDir(t *testing.T) {
	// Cannot run in parallel: mutates process-wide environment variables.

	tmp := t.TempDir()
	resolvedTmp, err := filepath.EvalSymlinks(tmp)
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", resolvedTmp)

		path, err := LogDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(resolvedTmp, "lstk"), path)
	} else {
		// Test XDG_STATE_HOME preference
		t.Setenv("XDG_STATE_HOME", resolvedTmp)

		path, err := LogDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(resolvedTmp, "lstk"), path)

		// Test fallback to HOME
		t.Setenv("XDG_STATE_HOME", "")
		fakeHome := t.TempDir()
		resolvedHome, _ := filepath.EvalSymlinks(fakeHome)
		t.Setenv("HOME", resolvedHome)

		path, err = LogDir()
		require.NoError(t, err)
		require.Equal(t, filepath.Join(resolvedHome, ".local", "state", "lstk"), path)
	}
}
