package proc

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunPropagatesExitCode verifies a wrapped tool's non-zero exit surfaces as
// an *exec.ExitError, matching cmd.Run's behaviour.
func TestRunPropagatesExitCode(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("sh", "-c", "exit 7")
	err := Run(cmd)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.ExitCode())
}

// TestRunSucceeds verifies a zero-exit tool returns nil.
func TestRunSucceeds(t *testing.T) {
	t.Parallel()

	require.NoError(t, Run(exec.Command("sh", "-c", "exit 0")))
}

// TestRunDoesNotKillOnContextCancel is the regression test for the reported bug:
// a cancelled context must no longer SIGKILL the wrapped tool. The child sleeps
// briefly then exits 0 on its own; if Run left exec.CommandContext's default
// Cancel in place, the cancellation would kill the child and Run would report a
// non-nil error (a signal-terminated ExitError or context.Canceled) instead of
// the clean exit.
func TestRunDoesNotKillOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 0.3; exit 0")

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Run(cmd)
	require.NoError(t, err)
	assert.True(t, cmd.ProcessState.Success(), "child should have exited on its own, not been killed")
}

// TestRunPreservesExitCodeOnContextCancel ensures the tool's real exit code
// survives a mid-run cancellation rather than being clobbered by context.Canceled.
func TestRunPreservesExitCodeOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", "sleep 0.3; exit 5")

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Run(cmd)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 5, exitErr.ExitCode())
	assert.False(t, errors.Is(err, context.Canceled))
}
