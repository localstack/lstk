package proc

import (
	"errors"
	"os/exec"
)

// userToolExitError is transparent — Error and Unwrap delegate — so callers
// keep seeing the *exec.ExitError they always did.
type userToolExitError struct{ err error }

func (e *userToolExitError) Error() string { return e.err.Error() }
func (e *userToolExitError) Unwrap() error { return e.err }

// MarkUserToolExit marks a wrapped tool the user asked for exiting non-zero, so
// telemetry attributes the failure to them rather than to lstk.
//
// Call it only where the invocation is the user's; running through Run is not
// that claim. lstk shells out for its own purposes too, and marking one of
// those hides an lstk bug behind the user's name.
func MarkUserToolExit(err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}
	return &userToolExitError{err: err}
}

// IsUserToolExit backs result.proxy_error on lstk_command telemetry events
// (DEVX-1004).
func IsUserToolExit(err error) bool {
	var userTool *userToolExitError
	return errors.As(err, &userTool)
}
