package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetInFileAppendsWhenKeyAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := `# User comment
[[containers]]
type = "aws"
port = "4566"
`
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v1.2.3"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(got)

	assert.Contains(t, result, "# User comment")
	assert.Contains(t, result, `type = "aws"`)
	assert.Contains(t, result, "[cli]")
	assert.Contains(t, result, `update_skipped_version = 'v1.2.3'`)

	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(got, &parsed))
}

func TestSetInFileReplacesExistingKeyInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := `# Keep this
[[containers]]
type = "aws"

[cli]
update_skipped_version = "v1.0.0"
`
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v2.0.0"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(got)

	assert.Contains(t, result, "# Keep this")
	assert.Contains(t, result, `update_skipped_version = 'v2.0.0'`)
	assert.NotContains(t, result, "v1.0.0")

	var parsed map[string]any
	require.NoError(t, toml.Unmarshal(got, &parsed))
}

func TestSetInFilePreservesCommentsAndFormatting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := `# lstk configuration file
# Run 'lstk config path' to see where this file lives.

[[containers]]
type = "aws"     # Emulator type
tag  = "latest"  # Docker image tag
port = "4566"    # Host port

# Example profiles:
# [env.debug]
# DEBUG = "1"
`
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v1.2.3"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(got)

	for _, want := range []string{
		"# lstk configuration file",
		"# Run 'lstk config path' to see where this file lives.",
		"# Emulator type",
		"# Docker image tag",
		"# Host port",
		"# Example profiles:",
		`# DEBUG = "1"`,
	} {
		assert.Contains(t, result, want, "expected comment preserved: %q", want)
	}
}

func TestSetInFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[[containers]]\ntype = \"aws\"\n"), 0644))

	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v1.0.0"))
	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v2.0.0"))
	require.NoError(t, setInFile(path, "cli.update_skipped_version", "v3.0.0"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed struct {
		CLI struct {
			UpdateSkippedVersion string `toml:"update_skipped_version"`
		} `toml:"cli"`
	}
	require.NoError(t, toml.Unmarshal(got, &parsed))
	assert.Equal(t, "v3.0.0", parsed.CLI.UpdateSkippedVersion)

	assert.Equal(t, 1, strings.Count(string(got), "update_skipped_version"))
}

func TestSetInFileErrorsOnMissingFile(t *testing.T) {
	err := setInFile(filepath.Join(t.TempDir(), "nonexistent.toml"), "cli.update_skipped_version", "v1.0.0")
	assert.Error(t, err)
}
