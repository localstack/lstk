package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetEmulatorType_WritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SetEmulatorType(EmulatorSnowflake))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "snowflake"`)
	assert.NotContains(t, string(got), `type = "aws"`)

	cfg, err := Get()
	require.NoError(t, err)
	require.Len(t, cfg.Containers, 1)
	assert.Equal(t, EmulatorSnowflake, cfg.Containers[0].Type)
}

func TestSetEmulatorType_NoOpWhenSameEmulator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SetEmulatorType(EmulatorAWS))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

func TestSetEmulatorType_PreservesInlineComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"     # Emulator type\ntag  = \"latest\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SetEmulatorType(EmulatorSnowflake))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "snowflake"     # Emulator type`)
}
