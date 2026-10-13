package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeAndLoad(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(viper.Reset)
	return path
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func TestSetEnvProfileVar_MultiLineEnvList(t *testing.T) {
	path := writeAndLoad(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\n  \"mine\",\n]\n\n[env.mine]\nDEBUG = \"1\"\n")

	changed, err := SetEnvProfileVar("p", "KEY", "1")

	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\n  \"mine\", \"p\",\n]\n\n[env.mine]\nDEBUG = \"1\"\n\n[env.p]\nKEY = \"1\"\n", readConfig(t, path))
}

func TestSetEnvProfileVar_RestoresFileWhenALaterProfileOverrides(t *testing.T) {
	original := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"p\", \"later\"]\n\n[env.p]\nKEY = \"0\"\n\n[env.later]\nKEY = \"0\"\n"
	path := writeAndLoad(t, original)

	_, err := SetEnvProfileVar("p", "KEY", "1")

	require.Error(t, err)
	assert.Equal(t, original, readConfig(t, path))
}

func TestSetEnvProfileVar_RestoresFileWhenResultDoesNotParse(t *testing.T) {
	// The dotted key already defines the table, so adding a header defines it twice.
	original := "env.p.OTHER = \"x\"\n\n[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	path := writeAndLoad(t, original)

	_, err := SetEnvProfileVar("p", "KEY", "1")

	require.Error(t, err)
	assert.Equal(t, original, readConfig(t, path))
}
