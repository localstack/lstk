//go:build windows

package proc

import (
	"errors"
	"io"
	"os/exec"
)

// RunInPTY is unix-only (see pty_unix.go): the wiring there — a creack/pty
// pseudo-terminal plus Setsid/Setctty — has no Windows counterpart. started is
// always false so callers fall back to Run.
func RunInPTY(_ *exec.Cmd, _ io.Writer) (started bool, err error) {
	return false, errors.ErrUnsupported
}
