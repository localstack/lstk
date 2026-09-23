package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCheckForUpdateOnStartup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    bool
		wantErr bool
	}{
		{"true", "true", true, false},
		{"false", "false", false, false},
		{"1", "1", true, false},
		{"0", "0", false, false},
		{"TRUE", "TRUE", true, false},
		// A plausible-sounding non-value: it must be reported, not coerced.
		{"unknown value", "quiet", false, true},
		{"empty", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseCheckForUpdateOnStartup(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "check_for_update_on_startup")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// loadConfigWithCLISection writes a config.toml with the given [cli] body and
// loads it. Not parallel-safe: viper state is global.
func loadConfigWithCLISection(t *testing.T, cliBody string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"
	if cliBody != "" {
		content += "\n[cli]\n" + cliBody + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, InitFromPath(path))
}

func TestGetRejectsNonBooleanCheckForUpdateOnStartup(t *testing.T) {
	loadConfigWithCLISection(t, `check_for_update_on_startup = "quiet"`)

	_, err := Get()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "check_for_update_on_startup")
}

func TestGetReadsCheckForUpdateOnStartup(t *testing.T) {
	loadConfigWithCLISection(t, `check_for_update_on_startup = false`)

	cfg, err := Get()
	require.NoError(t, err)
	require.NotNil(t, cfg.CLI.CheckForUpdateOnStartup)
	assert.False(t, *cfg.CLI.CheckForUpdateOnStartup)
}

// An unset key must stay distinguishable from an explicit false, so the
// default can apply.
func TestGetLeavesCheckForUpdateOnStartupNilWhenUnset(t *testing.T) {
	loadConfigWithCLISection(t, "")

	cfg, err := Get()
	require.NoError(t, err)
	assert.Nil(t, cfg.CLI.CheckForUpdateOnStartup)
}

func TestSetCheckForUpdateOnStartupPersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "# my config\n[[containers]]\ntype = \"aws\"\n\n[cli]\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, InitFromPath(path))

	require.NoError(t, SetCheckForUpdateOnStartup(false))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "check_for_update_on_startup = false")
	assert.Contains(t, string(data), "# my config", "existing comments must survive")
}

// Backs the prompt's opt-out: a dropped write would tell the user their choice
// was saved when the next run would prompt again.
func TestSetCheckForUpdateOnStartupFailsWithoutAConfigFile(t *testing.T) {
	viper.Reset()

	assert.False(t, HasFile())
	require.Error(t, SetCheckForUpdateOnStartup(false))
}

func TestHasFileReportsAResolvedConfig(t *testing.T) {
	loadConfigWithCLISection(t, "")

	assert.True(t, HasFile())
}

// The shipped template must stay writable: lstk inserts the key directly below
// the [cli] header, and the result has to read back correctly.
func TestSetCheckForUpdateOnStartupOnTheShippedTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(defaultConfigTemplate), 0644))
	require.NoError(t, InitFromPath(path))

	require.NoError(t, SetCheckForUpdateOnStartup(false))

	require.NoError(t, InitFromPath(path))
	cfg, err := Get()
	require.NoError(t, err)
	require.NotNil(t, cfg.CLI.CheckForUpdateOnStartup)
	assert.False(t, *cfg.CLI.CheckForUpdateOnStartup)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# CLI behavior")
}
