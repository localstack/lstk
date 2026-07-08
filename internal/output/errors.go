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

// ExitCodeError wraps an error with the explicit process exit code it should
// produce, mirroring how *exec.ExitError already carries a proxied command's
// exit code. Used for the JSON envelope's exit-code conventions (0/1/2/3/4) —
// main.go checks for this type the same way it already checks for
// *exec.ExitError.
type ExitCodeError struct {
	Err  error
	Code int
}

func (e *ExitCodeError) Error() string {
	return e.Err.Error()
}

func (e *ExitCodeError) Unwrap() error {
	return e.Err
}
