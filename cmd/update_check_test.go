package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestResolveUpdateCheckEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		envValue  string
		confValue *bool
		want      bool
	}{
		{"neither set defaults to enabled", "", nil, true},
		{"config false", "", boolPtr(false), false},
		{"config true", "", boolPtr(true), true},
		{"env false", "false", nil, false},
		{"env wins over config", "true", boolPtr(false), true},
		{"env false wins over config true", "false", boolPtr(true), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveUpdateCheckEnabled(
				&env.Env{CheckForUpdateOnStartup: tt.envValue},
				&config.Config{CLI: config.CLIConfig{CheckForUpdateOnStartup: tt.confValue}},
			)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// "quiet" is not a boolean — it stands in for a plausible-sounding typo, to
// prove a bad value is reported rather than silently disabling the check.
func TestResolveUpdateCheckEnabledRejectsInvalidEnvValue(t *testing.T) {
	t.Parallel()

	_, err := resolveUpdateCheckEnabled(
		&env.Env{CheckForUpdateOnStartup: "quiet"},
		&config.Config{},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), env.CheckForUpdateOnStartupVar)
	assert.Contains(t, err.Error(), "quiet")
}

// An invalid env value must be rejected even when the config file holds a good
// one: falling back would hide the typo and apply a setting not asked for.
func TestResolveUpdateCheckEnabledRejectsInvalidEnvValueOverValidConfig(t *testing.T) {
	t.Parallel()

	_, err := resolveUpdateCheckEnabled(
		&env.Env{CheckForUpdateOnStartup: "quiet"},
		&config.Config{CLI: config.CLIConfig{CheckForUpdateOnStartup: boolPtr(true)}},
	)

	require.Error(t, err)
}
