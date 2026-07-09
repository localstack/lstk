package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadFirstRunAfterConfigDirRecreated reproduces the scenario where a user
// deletes ~/.config/lstk/ and then runs lstk. newLogger() recreates the directory
// (for lstk.log) before config.Load() is called, so Load() must detect the
// MISSING FILE — not just the directory — to correctly set firstRun=true.
func TestLoadFirstRunAfterConfigDirRecreated(t *testing.T) {
	// Cannot run in parallel: mutates process-wide HOME env and viper state.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	viper.Reset()
	t.Cleanup(viper.Reset)

	// ~/.config/ exists so the creation dir is ~/.config/lstk (not the OS default).
	homeConfig := filepath.Join(fakeHome, ".config")
	require.NoError(t, os.MkdirAll(homeConfig, 0755))

	// Simulate newLogger() recreating ~/.config/lstk/ (with a log file) after
	// the user deleted the whole directory but before config.Load() runs.
	configDir := filepath.Join(homeConfig, "lstk")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "lstk.log"), []byte("log\n"), 0600))
	// config.toml is intentionally absent — the user deleted it along with the dir.

	firstRun, err := Load()

	require.NoError(t, err)
	assert.True(t, firstRun, "Load() should return firstRun=true when the config directory exists but config.toml does not")
}

// TestEnsureCreatedPrefersHomeConfigDirWhenPresent and
// TestEnsureCreatedFallsBackToOSConfigDirWhenHomeConfigMissing cover the
// config-creation path-resolution policy directly (rather than through an
// arbitrary CLI command): $HOME/.config/lstk when $HOME/.config already
// exists, otherwise the OS default. Only `start`/bare `lstk` ever call
// EnsureCreated on a genuine first run — see the "Choosing an emulator" note
// in CLAUDE.md — so this is tested at the config-package level instead of by
// running some other command that happens to trigger it.
func TestEnsureCreatedPrefersHomeConfigDirWhenPresent(t *testing.T) {
	// Cannot run in parallel: mutates process-wide HOME env and viper state.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	viper.Reset()
	t.Cleanup(viper.Reset)

	require.NoError(t, os.MkdirAll(filepath.Join(fakeHome, ".config"), 0755))

	firstRun, err := Load()
	require.NoError(t, err)
	require.True(t, firstRun)
	require.NoError(t, EnsureCreated())

	expectedConfigFile := filepath.Join(fakeHome, ".config", "lstk", "config.toml")
	assert.FileExists(t, expectedConfigFile)
	assertDefaultConfigContent(t, expectedConfigFile)
}

func TestEnsureCreatedFallsBackToOSConfigDirWhenHomeConfigMissing(t *testing.T) {
	// Cannot run in parallel: mutates process-wide HOME env and viper state.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("XDG_CONFIG_HOME", "")
	viper.Reset()
	t.Cleanup(viper.Reset)

	firstRun, err := Load()
	require.NoError(t, err)
	require.True(t, firstRun)
	require.NoError(t, EnsureCreated())

	osConfigDir, err := osConfigDir()
	require.NoError(t, err)
	expectedConfigFile := filepath.Join(osConfigDir, "config.toml")
	assert.FileExists(t, expectedConfigFile)
	assertDefaultConfigContent(t, expectedConfigFile)
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

func TestSetInFileAddsSecondFieldToExistingSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := `# Keep this
[[containers]]
type = "aws"

[cli]
update_skipped_version = "v1.0.0"
`
	require.NoError(t, os.WriteFile(path, []byte(original), 0644))

	require.NoError(t, setInFile(path, "cli.notify_cadence", "minor"))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	result := string(got)

	var parsed struct {
		CLI struct {
			UpdateSkippedVersion string `toml:"update_skipped_version"`
			NotifyCadence        string `toml:"notify_cadence"`
		} `toml:"cli"`
	}
	require.NoError(t, toml.Unmarshal(got, &parsed))
	assert.Equal(t, "v1.0.0", parsed.CLI.UpdateSkippedVersion)
	assert.Equal(t, "minor", parsed.CLI.NotifyCadence)

	assert.Equal(t, 1, strings.Count(result, "[cli]"), "expected a single [cli] section header")
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
