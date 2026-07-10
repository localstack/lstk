package container

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTempConfig writes content to a temp config.toml and loads it as the active
// config, returning its path. These tests mutate the process-global viper state,
// so they must not run in parallel.
func loadTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, config.InitFromPath(path))
	return path
}

func TestApplyEmulatorType_SwitchesInPlace(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"     # keep me\ntag = \"latest\"\nport = \"4566\"\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	containers, err := ApplyEmulatorType(output.NewPlainSink(&buf), config.EmulatorAzure, cfg.Containers, false, path)
	require.NoError(t, err)

	require.Len(t, containers, 1)
	assert.Equal(t, config.EmulatorAzure, containers[0].Type)
	assert.Contains(t, buf.String(), "Switched configured emulator to azure")

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), `type = "azure"`)
	assert.Contains(t, string(data), "# keep me")
}

func TestApplyEmulatorType_NoOpWhenMatching(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	path := loadTempConfig(t, content)
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	containers, err := ApplyEmulatorType(output.NewPlainSink(&buf), config.EmulatorAWS, cfg.Containers, false, path)
	require.NoError(t, err)

	assert.Equal(t, config.EmulatorAWS, containers[0].Type)
	assert.Empty(t, buf.String())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestApplyEmulatorType_ErrorsWhenImageSet(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\nimage = \"my-registry.example.com/localstack-pro:3.0\"\n"
	path := loadTempConfig(t, content)
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = ApplyEmulatorType(output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.Error(t, err)
	assert.True(t, output.IsSilent(err))
	assert.Contains(t, buf.String(), "custom image")

	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

func TestApplyEmulatorType_WarnsOnTagAndVolumes(t *testing.T) {
	path := loadTempConfig(t, "[[containers]]\ntype = \"aws\"\ntag = \"3.0\"\nport = \"4566\"\nvolumes = [\"./init.sql:/etc/localstack/init/ready.d/init.sql\"]\n")
	cfg, err := config.Get()
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = ApplyEmulatorType(output.NewPlainSink(&buf), config.EmulatorSnowflake, cfg.Containers, false, path)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `Keeping tag "3.0"`)
	assert.Contains(t, out, "Keeping volume mounts")
	assert.Contains(t, out, "Switched configured emulator to snowflake")
}
