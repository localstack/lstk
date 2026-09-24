//go:build !windows

package proc

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"

	"github.com/creack/pty"
	"github.com/muesli/cancelreader"
	"golang.org/x/term"

	"github.com/localstack/lstk/internal/terminal"
)

// RunInPTY runs cmd like Run, but gives the child a pseudo-terminal instead of
// a plain pipe for stdout/stderr, copying its output to out. Over a pipe,
// e.g. the Python aws CLI block-buffers stdout (ignoring PYTHONUNBUFFERED),
// holding back streaming commands like `logs tail --follow` until exit; a PTY
// makes it detect a terminal and stay line-buffered (DEVX-1026).
//
// When cmd.Stdin is itself a terminal, the PTY additionally becomes the
// child's stdin and controlling terminal, with keystrokes pumped into it from
// the real terminal — see wireInteractiveStdin for why (DEVX-1049). Otherwise
// (stdin redirected, `yes | lstk aws ...`) stdin stays wired as given and no
// new session is created, so piped data never passes through a terminal's
// line discipline and the child stays in lstk's process group, still getting
// Ctrl-C directly. started is false if no PTY could be allocated; the caller
// should fall back to Run.
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

	restore, interrupted := wireInteractiveStdin(cmd, ptmx, tty)

	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(copied)
	}()

	runErr := Run(cmd)

	// Wait for the copy goroutine to drain remaining output and hit EOF/EIO.
	_ = tty.Close()
	<-copied

	// A failure to hand the terminal back is the run's failure too, but never
	// louder than the child's own error.
	if restoreErr := restore(); restoreErr != nil && runErr == nil {
		runErr = restoreErr
	}
	if interrupted() {
		runErr = markInterrupted(runErr)
	}
	return true, runErr
}

// wireInteractiveStdin makes the PTY the child's terminal for input as well as
// output when cmd.Stdin is a real terminal, returning a func that restores
// that terminal's state (a no-op otherwise).
//
// Output alone is not enough: the aws CLI pages long output through less, and
// less (577 and newer) reads keyboard input from ttyname(2) — the terminal
// attached to its stderr, i.e. this PTY — falling back to /dev/tty, then fd 2. With
// nothing feeding the PTY's master side, every keystroke sat unread on the
// user's terminal and the pager looked frozen (DEVX-1049). So the user's
// terminal is switched to raw mode and its bytes pumped into the PTY, whose
// line discipline becomes the single place where echo, line buffering, and
// signal keys take effect — under the child's control, which is what a pager
// setting raw mode expects.
//
// Raw mode also stops the user's terminal from turning Ctrl-C into SIGINT, so
// the child is made a session leader with the PTY as its controlling terminal
// (Setsid+Setctty): the ^C byte is pumped through and the PTY's line
// discipline delivers SIGINT to the child — without a session attached to the
// PTY it would be delivered to nobody. lstk itself no longer sees that SIGINT,
// matching how Run already declines to forward SIGINT while attached to a
// terminal.
//
// The pump reads through a cancelreader so restore can stop it and join the
// goroutine: a plain read on the user's terminal cannot be interrupted, and a
// pump left blocked past the child's exit would steal the next bytes typed at
// whatever reads the terminal after lstk (see
// TestRunInPTYStopsReadingTerminalAfterExit).
//
// interrupted reports whether the pump forwarded a Ctrl-C (ETX) byte: since
// lstk no longer receives SIGINT here, that byte is the only record that the
// user interrupted the run (see WasInterrupted).
func wireInteractiveStdin(cmd *exec.Cmd, ptmx, tty *os.File) (restore func() error, interrupted func() bool) {
	noop := func() error { return nil }
	never := func() bool { return false }
	stdin, ok := cmd.Stdin.(*os.File)
	if !ok || !terminal.IsTerminal(stdin) {
		return noop, never
	}
	prev, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		// A terminal that refuses raw mode keeps the old output-only wiring:
		// the pager stays unresponsive there, but nothing else regresses.
		return noop, never
	}
	reader, err := cancelreader.NewReader(stdin)
	if err != nil {
		// Without a cancelable reader the pump could not be torn down; keep
		// the output-only wiring here too.
		_ = term.Restore(int(stdin.Fd()), prev)
		return noop, never
	}

	cmd.Stdin = tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true // Ctty 0: the child's fd 0, the tty above

	pumped := make(chan struct{})
	pump := &interruptWatcher{w: ptmx}
	go func() {
		_, _ = io.Copy(pump, reader)
		close(pumped)
	}()

	restore = func() error {
		reader.Cancel()
		<-pumped
		_ = reader.Close()
		return term.Restore(int(stdin.Fd()), prev)
	}
	return restore, func() bool { return pump.seen.Load() }
}

// interruptWatcher forwards pumped bytes and remembers whether an ETX (Ctrl-C
// in raw mode) went through.
type interruptWatcher struct {
	w    io.Writer
	seen atomic.Bool
}

func (p *interruptWatcher) Write(b []byte) (int, error) {
	if bytes.IndexByte(b, 0x03) >= 0 {
		p.seen.Store(true)
	}
	return p.w.Write(b)
}
