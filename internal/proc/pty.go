package proc

import (
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// RunInPTY runs cmd like Run, but gives the child a pseudo-terminal instead of
// a plain pipe for stdout/stderr, copying its output to out. Over a pipe,
// e.g. the Python aws CLI block-buffers stdout (ignoring PYTHONUNBUFFERED),
// holding back streaming commands like `logs tail --follow` until exit; a PTY
// makes it detect a terminal and stay line-buffered (DEVX-1026).
//
// No new session is created, so the child stays in lstk's process group and
// still gets Ctrl-C directly. started is false if no PTY could be allocated
// (e.g. Windows); the caller should fall back to Run.
func RunInPTY(cmd *exec.Cmd, out io.Writer) (started bool, err error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return false, err
	}
	defer func() { _ = ptmx.Close() }()

	if size, sizeErr := pty.GetsizeFull(os.Stdout); sizeErr == nil {
		_ = pty.Setsize(ptmx, size)
	}

	cmd.Stdout = tty
	cmd.Stderr = tty

	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(copied)
	}()

	runErr := Run(cmd)

	// Wait for the copy goroutine to drain remaining output and hit EOF/EIO.
	_ = tty.Close()
	<-copied
	return true, runErr
}
