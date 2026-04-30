package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwitchEmulatorContent_NoOp_AlreadyAWS(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	result, changed, err := switchEmulatorContent(content, EmulatorAWS)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, content, result)
}

func TestSwitchEmulatorContent_NoOp_AlreadySnowflake(t *testing.T) {
	content := "[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n"
	result, changed, err := switchEmulatorContent(content, EmulatorSnowflake)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, content, result)
}

func TestSwitchEmulatorContent_CommentAWSAndAppendSnowflake(t *testing.T) {
	content := `[[containers]]
type = "aws"
port = "4566"

[cli]
update_skipped_version = ""
`
	result, changed, err := switchEmulatorContent(content, EmulatorSnowflake)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, result, "# [[containers]]")
	assert.Contains(t, result, `# type = "aws"`)
	assert.Contains(t, result, `# port = "4566"`)
	assert.Contains(t, result, `type = "snowflake"`)
	assert.Contains(t, result, "[cli]")
	// aws block should not appear as active
	assert.NotContains(t, result, "\n[[containers]]\ntype = \"aws\"")
}

func TestSwitchEmulatorContent_RestoresCommentedAWS(t *testing.T) {
	content := "# [[containers]]\n# type = \"aws\"\n# port = \"4566\"\n\n[[containers]]\ntype = \"snowflake\"\nport = \"4566\"\n"
	result, changed, err := switchEmulatorContent(content, EmulatorAWS)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, result, "[[containers]]\ntype = \"aws\"")
	assert.Contains(t, result, "# [[containers]]")
	assert.Contains(t, result, `# type = "snowflake"`)
	assert.NotContains(t, result, "\n[[containers]]\ntype = \"snowflake\"")
}

func TestSwitchEmulatorContent_RestoresCommentedSnowflake(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n\n# [[containers]]\n# type = \"snowflake\"\n# port = \"4566\"\n"
	result, changed, err := switchEmulatorContent(content, EmulatorSnowflake)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, result, "[[containers]]\ntype = \"snowflake\"")
	assert.Contains(t, result, "# [[containers]]")
	assert.Contains(t, result, `# type = "aws"`)
}

func TestSwitchEmulatorContent_PreservesNonContainerContent(t *testing.T) {
	content := `# lstk configuration file

[[containers]]
type = "aws"
port = "4566"
# volume = ""    # some comment

# [env.debug]
# DEBUG = "1"

[cli]
update_skipped_version = "v1.2.3"
`
	result, changed, err := switchEmulatorContent(content, EmulatorSnowflake)
	require.NoError(t, err)
	assert.True(t, changed)

	assert.Contains(t, result, "# lstk configuration file")
	assert.Contains(t, result, `update_skipped_version = "v1.2.3"`)
	assert.Contains(t, result, "# [env.debug]")
	assert.Contains(t, result, `type = "snowflake"`)
}

func TestSwitchEmulatorContent_PreservesInlineComments(t *testing.T) {
	content := "[[containers]]\ntype = \"aws\"     # Emulator type\ntag  = \"latest\"  # Docker image tag\nport = \"4566\"    # Host port\n# volume = \"\"    # persistent state\n"
	result, changed, err := switchEmulatorContent(content, EmulatorSnowflake)
	require.NoError(t, err)
	assert.True(t, changed)

	// Original inline comments should be preserved in the commented-out block
	assert.Contains(t, result, "# type = \"aws\"     # Emulator type")
	assert.Contains(t, result, "# # volume = \"\"    # persistent state")
}

func TestSwitchEmulatorContent_RoundTrip(t *testing.T) {
	original := `[[containers]]
type = "aws"
port = "4566"
`
	// Switch to snowflake
	afterSnowflake, changed, err := switchEmulatorContent(original, EmulatorSnowflake)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, afterSnowflake, `type = "snowflake"`)

	// Switch back to AWS — should restore the commented block
	afterAWS, changed, err := switchEmulatorContent(afterSnowflake, EmulatorAWS)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Contains(t, afterAWS, "[[containers]]\ntype = \"aws\"")
	assert.NotContains(t, afterAWS, "\n[[containers]]\ntype = \"snowflake\"")
}

func TestSwitchEmulator_WritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SwitchEmulator(EmulatorSnowflake))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(got), `type = "snowflake"`)
	assert.True(t, strings.Contains(string(got), "# [[containers]]"))

	cfg, err := Get()
	require.NoError(t, err)
	require.Len(t, cfg.Containers, 1)
	assert.Equal(t, EmulatorSnowflake, cfg.Containers[0].Type)
}

func TestSwitchEmulator_NoOpWhenSameEmulator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\nport = \"4566\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, loadConfig(path))
	t.Cleanup(func() { viper.Reset() })

	require.NoError(t, SwitchEmulator(EmulatorAWS))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}
