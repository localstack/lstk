package output

import "errors"

// SilentError wraps an error that has already been displayed to the user
// through the sink mechanism. Callers should check for this type and skip
// printing the error again.
type SilentError struct {
	Err error
}

func (e *SilentError) Error() string {
	return e.Err.Error()
}

func (e *SilentError) Unwrap() error {
	return e.Err
}

func NewSilentError(err error) *SilentError {
	return &SilentError{Err: err}
}

// IsSilent returns true if the error (or any error in its chain) is a SilentError.
func IsSilent(err error) bool {
	var silent *SilentError
	return errors.As(err, &silent)
}

// ExitCodeError carries a specific process exit code through the error chain.
// Used by passthrough commands (e.g. lstk aws) to propagate the subprocess exit code.
type ExitCodeError struct {
	Code int
	Err  error
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}

func NewExitCodeError(code int, err error) *ExitCodeError {
	return &ExitCodeError{Code: code, Err: err}
}

// ExitCode returns the exit code if err (or any error in its chain) is an ExitCodeError,
// or 1 as a default.
func ExitCode(err error) int {
	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}
