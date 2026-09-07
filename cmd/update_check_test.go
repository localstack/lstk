package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUpdateCheckMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		envValue  string
		confValue string
		want      config.UpdateCheckMode
	}{
		{"neither set stays unset so detection can decide", "", "", config.UpdateCheckUnset},
		{"config only", "", "notify", config.UpdateCheckNotify},
		{"env only", "off", "", config.UpdateCheckOff},
		{"env wins over config", "prompt", "off", config.UpdateCheckPrompt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveUpdateCheckMode(
				&env.Env{UpdateCheck: tt.envValue},
				&config.Config{CLI: config.CLIConfig{UpdateCheck: tt.confValue}},
			)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// "quiet" is not a mode — it stands in for a plausible-sounding typo, to prove
// a bad value is reported rather than silently coerced into some default.
func TestResolveUpdateCheckModeRejectsInvalidEnvValue(t *testing.T) {
	t.Parallel()

	_, err := resolveUpdateCheckMode(
		&env.Env{UpdateCheck: "quiet"},
		&config.Config{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), env.UpdateCheckVar)
	assert.Contains(t, err.Error(), "quiet")
}

// An invalid env value must be rejected even when the config file holds a
// perfectly good one: silently falling back would hide the user's typo and
// apply a mode they did not ask for.
func TestResolveUpdateCheckModeRejectsInvalidEnvValueOverValidConfig(t *testing.T) {
	t.Parallel()

	_, err := resolveUpdateCheckMode(
		&env.Env{UpdateCheck: "quiet"},
		&config.Config{CLI: config.CLIConfig{UpdateCheck: "notify"}},
	)

	require.Error(t, err)
}
