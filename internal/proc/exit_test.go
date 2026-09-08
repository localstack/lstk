package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The half that is easy to lose: an *exec.ExitError alone is not the user's
// tool exiting, since lstk's own helper execs produce one too.
func TestIsUserToolExitOnlyForMarkedToolExits(t *testing.T) {
	t.Parallel()

	t.Run("nil is not a user tool exit", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsUserToolExit(nil))
		assert.NoError(t, MarkUserToolExit(nil))
	})

	t.Run("plain error is not a user tool exit", func(t *testing.T) {
		t.Parallel()

		err := errors.New("runtime not healthy")
		assert.False(t, IsUserToolExit(err))
		assert.False(t, IsUserToolExit(MarkUserToolExit(err)), "only a tool exit is the tool's fault")
	})

	t.Run("unmarked ExitError is not a user tool exit", func(t *testing.T) {
		t.Parallel()
		requireUnixShell(t)

		err := exec.Command("sh", "-c", "exit 7").Run()
		require.Error(t, err)
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.False(t, IsUserToolExit(err), "lstk's own helper execs exit non-zero too")
		assert.False(t, IsUserToolExit(fmt.Errorf("update failed: %w", err)))
	})

	t.Run("marked tool exit survives the caller's wrapping", func(t *testing.T) {
		t.Parallel()
		requireUnixShell(t)

		err := MarkUserToolExit(Run(exec.Command("sh", "-c", "exit 252")))
		require.Error(t, err)
		assert.True(t, IsUserToolExit(err))
		assert.True(t, IsUserToolExit(fmt.Errorf("terraform: %w", err)))
	})

	t.Run("missing binary is not a user tool exit", func(t *testing.T) {
		t.Parallel()

		err := MarkUserToolExit(Run(exec.Command("lstk-no-such-binary-devx1004")))
		require.Error(t, err)
		assert.False(t, IsUserToolExit(err), "the tool never ran, so its exit cannot be the cause")
	})
}

// The marker must stay transparent: the proxy exec paths' errors.As checks,
// cmd.ExitCode, and the telemetry error_msg all read through it.
func TestMarkUserToolExitPreservesExitError(t *testing.T) {
	t.Parallel()
	requireUnixShell(t)

	err := MarkUserToolExit(Run(exec.Command("sh", "-c", "exit 252")))
	require.Error(t, err)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 252, exitErr.ExitCode())
	assert.Equal(t, "exit status 252", err.Error())
}
