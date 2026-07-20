package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEmulatorType(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    EmulatorType
		wantErr bool
	}{
		{"aws", EmulatorAWS, false},
		{"snowflake", EmulatorSnowflake, false},
		{"azure", EmulatorAzure, false},
		{"AWS", "", true},
		{"", "", true},
		{"bogus", "", true},
	} {
		got, err := ParseEmulatorType(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q", tc.in)
			continue
		}
		require.NoError(t, err, "input %q", tc.in)
		assert.Equal(t, tc.want, got)
	}
}

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

// TestSetEmulatorType_OnlyRewritesActiveBlock guards the surgical rewrite: only
// the active block's type line changes. A commented-out example block and an
// unrelated `content_type` key (which both contain the substring `type = "..."`)
// must be left untouched.
func TestSetEmulatorType_OnlyRewritesActiveBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\nenv = [\"p\"]\n\n# [[containers]]\n# type = \"snowflake\"\n\n[env.p]\ncontent_type = \"json\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SetEmulatorType(EmulatorAzure))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), "type = \"azure\"\nport", "active block should switch")
	assert.Contains(t, string(got), `# type = "snowflake"`, "commented block must be untouched")
	assert.Contains(t, string(got), `content_type = "json"`, "unrelated key must be untouched")
}

// TestSetEmulatorType_IgnoresTypeKeyInEarlierTable guards the block-scoped
// rewrite: a `type` key in another table that appears before [[containers]]
// (e.g. an [env.*] profile) must not be mistaken for the emulator type. TOML
// tables have no required order, so the search must be anchored to the
// [[containers]] header, not just the first `type =` line in the file.
func TestSetEmulatorType_IgnoresTypeKeyInEarlierTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[env.default]\ntype = \"custom\"\n\n[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SetEmulatorType(EmulatorAzure))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "custom"`, "the env table's type key must be untouched")
	assert.Contains(t, string(got), `type = "azure"`, "the container block's type must switch")
	assert.NotContains(t, string(got), `type = "aws"`)
}

// TestSetEmulatorType_ErrorsWithoutContainersBlock ensures a config with no
// [[containers]] block reports a clear error rather than silently rewriting an
// unrelated key or reporting a generic "no type field" message.
func TestSetEmulatorType_ErrorsWithoutContainersBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[env.default]\ntype = \"custom\"\n"), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	err := SetEmulatorType(EmulatorAzure)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "[[containers]] block")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "custom"`, "the unrelated key must be left untouched on error")
}
