package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUpdateCheckMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    UpdateCheckMode
		wantErr bool
	}{
		{"prompt", "prompt", UpdateCheckPrompt, false},
		{"notify", "notify", UpdateCheckNotify, false},
		{"off", "off", UpdateCheckOff, false},
		{"empty means unset", "", UpdateCheckUnset, false},
		// A plausible-sounding non-mode: it must be reported, not coerced.
		{"unknown value", "quiet", "", true},
		{"case sensitive", "Off", "", true},
		{"whitespace is not trimmed away silently", " off", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUpdateCheckMode(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "update_check")
				assert.Contains(t, err.Error(), "prompt")
				assert.Contains(t, err.Error(), "notify")
				assert.Contains(t, err.Error(), "off")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// writeTestConfig writes a config.toml with the given [cli] body and loads it.
// Not parallel-safe: viper state is global.
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

func TestGetRejectsInvalidUpdateCheck(t *testing.T) {
	loadConfigWithCLISection(t, `update_check = "quiet"`)

	_, err := Get()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update_check")
	assert.Contains(t, err.Error(), "quiet")
}

func TestGetAcceptsValidUpdateCheck(t *testing.T) {
	loadConfigWithCLISection(t, `update_check = "notify"`)

	cfg, err := Get()
	require.NoError(t, err)
	assert.Equal(t, "notify", cfg.CLI.UpdateCheck)
}

func TestGetTreatsMissingUpdateCheckAsUnset(t *testing.T) {
	loadConfigWithCLISection(t, "")

	cfg, err := Get()
	require.NoError(t, err)
	assert.Empty(t, cfg.CLI.UpdateCheck)
}

func TestSetUpdateCheckPersistsToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "# my config\n[[containers]]\ntype = \"aws\"\n\n[cli]\nupdate_skipped_version = \"v1.2.3\"\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, InitFromPath(path))

	require.NoError(t, SetUpdateCheck(UpdateCheckNotify))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// setInFile encodes strings as TOML literal strings (single quotes); see
	// TestSetInFileAppendsWhenKeyAbsent.
	assert.Contains(t, string(data), `update_check = 'notify'`)
	assert.Contains(t, string(data), "# my config", "existing comments must survive")
	assert.Contains(t, string(data), `update_skipped_version = "v1.2.3"`, "sibling keys must survive")
}

// SetUpdateCheck must fail rather than succeed in memory only when there is no
// config file: it backs the "Never ask again" prompt option, and a silent no-op
// would tell the user their choice was saved when the next run would ask again.
func TestSetUpdateCheckFailsWithoutAConfigFile(t *testing.T) {
	viper.Reset()

	assert.False(t, HasFile())
	require.Error(t, SetUpdateCheck(UpdateCheckNotify))
}

func TestHasFileReportsAResolvedConfig(t *testing.T) {
	loadConfigWithCLISection(t, "")

	assert.True(t, HasFile())
}

// The shipped template documents update_check as a commented line inside
// [cli], while setInFile inserts a written key directly below the header — so
// the live value lands above the comment describing it. That is accepted
// (the alternative was a comment block detached from its own table), but the
// result must still be valid TOML that reads back correctly.
func TestSetUpdateCheckOnTheShippedTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(defaultConfigTemplate), 0644))
	require.NoError(t, InitFromPath(path))

	require.NoError(t, SetUpdateCheck(UpdateCheckNotify))

	require.NoError(t, InitFromPath(path))
	cfg, err := Get()
	require.NoError(t, err)
	assert.Equal(t, "notify", cfg.CLI.UpdateCheck)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "# CLI behavior")
}
