package proc

import (
	"io"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// RunInPTY runs cmd like Run, but with the child's stdout and stderr connected
// to the slave side of a newly allocated pseudo-terminal, copying everything
// the child writes to out. Wrapped CLIs then detect a terminal and keep
// line-buffered, colored, interactive output even though lstk sits between them
// and the user's terminal — with a plain io.Writer, os/exec hands the child a
// pipe, and e.g. the frozen Python aws CLI block-buffers stdout (8 KB, and it
// ignores PYTHONUNBUFFERED), holding back streaming commands like
// `logs tail --follow` until exit (DEVX-1026).
//
// stdout and stderr share the one PTY, exactly as they would share the user's
// terminal. cmd.Stdin is left untouched, and no new session is created, so the
// child stays in lstk's process group and still receives Ctrl-C from the
// terminal directly. The PTY size is inherited from lstk's terminal once at
// startup; resizes are not propagated.
//
// started reports whether the child ran: false means no PTY could be allocated
// (e.g. on Windows) and the caller should fall back to Run with plain writers.
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

	// Closing the parent's slave FD leaves no slave side open (the child's dup
	// died with it), so the copy goroutine drains what's buffered and gets
	// EOF/EIO; wait for it so out has everything before returning.
	_ = tty.Close()
	<-copied
	return true, runErr
}
