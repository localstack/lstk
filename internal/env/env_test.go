package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitReadsCheckForUpdateOnStartup(t *testing.T) {
	t.Setenv("LSTK_CHECK_FOR_UPDATE_ON_STARTUP", "false")

	cfg := Init()

	assert.Equal(t, "false", cfg.CheckForUpdateOnStartup)
}

// Kept raw and empty when unset, so the command boundary can tell an unset
// variable from an explicit false.
func TestInitLeavesCheckForUpdateOnStartupEmptyWhenUnset(t *testing.T) {
	t.Setenv("LSTK_CHECK_FOR_UPDATE_ON_STARTUP", "")

	cfg := Init()

	assert.Empty(t, cfg.CheckForUpdateOnStartup)
}
