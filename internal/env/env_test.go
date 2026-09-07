package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitReadsUpdateCheck(t *testing.T) {
	t.Setenv("LSTK_UPDATE_CHECK", "off")

	cfg := Init()

	assert.Equal(t, "off", cfg.UpdateCheck)
}

func TestInitLeavesUpdateCheckEmptyWhenUnset(t *testing.T) {
	// Explicitly cleared: without this the test reads the developer's own
	// environment and fails for anyone who exports the variable.
	t.Setenv("LSTK_UPDATE_CHECK", "")

	cfg := Init()

	assert.Empty(t, cfg.UpdateCheck)
}
