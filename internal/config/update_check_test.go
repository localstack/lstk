package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGet_UpdateCheckAcceptsKnownModes checks that prompt, notify, and off
// are valid cli.update_check values; anything else is rejected at load.
func TestGet_UpdateCheckAcceptsKnownModes(t *testing.T) {
	// Cannot run in parallel: mutates process-wide viper state.
	for _, mode := range []string{"prompt", "notify", "off"} {
		t.Run(mode, func(t *testing.T) {
			configFile := filepath.Join(t.TempDir(), configFileName)
			require.NoError(t, os.WriteFile(configFile, []byte(`
[[containers]]
type = "aws"
port = "4566"

[cli]
update_check = "`+mode+`"
`), 0600))

			viper.Reset()
			t.Cleanup(viper.Reset)
			viper.SetConfigFile(configFile)
			require.NoError(t, viper.ReadInConfig())

			cfg, err := Get()
			require.NoError(t, err)
			assert.Equal(t, mode, cfg.CLI.UpdateCheck)
		})
	}
}

func TestGet_UpdateCheckRejectsUnknownMode(t *testing.T) {
	configFile := filepath.Join(t.TempDir(), configFileName)
	require.NoError(t, os.WriteFile(configFile, []byte(`
[cli]
update_check = "bogus"
`), 0600))

	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.SetConfigFile(configFile)
	require.NoError(t, viper.ReadInConfig())

	_, err := Get()
	assert.ErrorContains(t, err, "cli.update_check")
}
