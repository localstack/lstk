package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveUpdateCheckMode checks the LSTK_UPDATE_CHECK > cli.update_check
// precedence: env var wins, and an empty result means neither was set.
func TestResolveUpdateCheckMode(t *testing.T) {
	t.Run("neither set: unset", func(t *testing.T) {
		mode, err := resolveUpdateCheckMode("", "")
		require.NoError(t, err)
		assert.Empty(t, mode)
	})

	t.Run("config value alone", func(t *testing.T) {
		mode, err := resolveUpdateCheckMode("", update.ModeOff)
		require.NoError(t, err)
		assert.Equal(t, update.ModeOff, mode)
	})

	t.Run("env var alone", func(t *testing.T) {
		mode, err := resolveUpdateCheckMode(update.ModeNotify, "")
		require.NoError(t, err)
		assert.Equal(t, update.ModeNotify, mode)
	})

	t.Run("env var wins over config value", func(t *testing.T) {
		mode, err := resolveUpdateCheckMode(update.ModeOff, update.ModeNotify)
		require.NoError(t, err)
		assert.Equal(t, update.ModeOff, mode)
	})

	t.Run("invalid env value is rejected", func(t *testing.T) {
		_, err := resolveUpdateCheckMode("bogus", "")
		assert.Error(t, err)
	})

	t.Run("invalid config value is rejected", func(t *testing.T) {
		_, err := resolveUpdateCheckMode("", "bogus")
		assert.Error(t, err)
	})
}
