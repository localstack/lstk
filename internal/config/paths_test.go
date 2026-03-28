package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestFriendlyConfigPathRelativeForProjectLocal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	configDir := filepath.Join(dir, ".lstk")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	configFile := filepath.Join(configDir, "config.toml")
	require.NoError(t, os.WriteFile(configFile, []byte("[aws]\n"), 0644))

	origDir, err := os.Getwd()
	require.NoError(t, err)

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	viper.Reset()
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	friendly, err := FriendlyConfigPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".lstk", "config.toml"), friendly)
}

func TestFriendlyConfigPathTildeForHomeDir(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	require.NoError(t, err)

	configDir := filepath.Join(home, ".config", "lstk")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Skip("~/.config/lstk does not exist")
	}

	configFile := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Skip("~/.config/lstk/config.toml does not exist")
	}

	viper.Reset()
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	friendly, err := FriendlyConfigPath()
	require.NoError(t, err)
	require.Equal(t, filepath.Join("~", ".config", "lstk", "config.toml"), friendly)
}
