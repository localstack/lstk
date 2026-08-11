package integration_test

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/xpty"
	"github.com/stretchr/testify/require"
)

// newTestPty opens a cross-platform PTY: a classic Unix PTY, or a ConPTY on
// Windows. On unix the PTY is left unsized, matching the previous creack
// pty.Start behavior the existing assertions were calibrated against. ConPTY
// renders output through a virtual screen buffer and re-wraps lines at its
// width, so a wide buffer keeps long lines (URLs, paths) intact for substring
// assertions.
func newTestPty(t *testing.T) xpty.Pty {
	t.Helper()
	width, height := -1, -1
	if runtime.GOOS == "windows" {
		width, height = 300, 80
	}
	pt, err := xpty.NewPty(width, height)
	require.NoError(t, err, "failed to open PTY")
	return pt
}

// ptyProc is a process running on a PTY with its combined output captured
// continuously, so tests can poll for prompts, type keystrokes, and assert on
// the final output.
type ptyProc struct {
	t    *testing.T
	ctx  context.Context
	cmd  *exec.Cmd
	pt   xpty.Pty
	out  *syncBuffer
	done chan struct{}
}

// startCmdInPTY starts an arbitrary command on a PTY. Most tests want
// startLstkInPTY or runLstkInPTY instead.
func startCmdInPTY(t *testing.T, ctx context.Context, cmd *exec.Cmd) *ptyProc {
	t.Helper()

	pt := newTestPty(t)
	require.NoError(t, pt.Start(cmd), "failed to start command in PTY")
	if up, ok := pt.(*xpty.UnixPty); ok {
		// Close the parent's copy of the slave end so reads on the master
		// return EOF once the child exits (creack's pty.Start does the same;
		// xpty keeps it open).
		_ = up.Slave().Close()
	}

	p := &ptyProc{t: t, ctx: ctx, cmd: cmd, pt: pt, out: &syncBuffer{}, done: make(chan struct{})}
	go func() {
		_, _ = io.Copy(p.out, pt)
		close(p.done)
	}()
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
		_ = pt.Close()
	})
	return p
}

// startLstkInPTY starts the lstk binary on a PTY so that ui.IsInteractive()
// returns true. Use the returned ptyProc to interact with prompts; call wait()
// to collect the output and exit status.
func startLstkInPTY(t *testing.T, ctx context.Context, environ []string, args ...string) *ptyProc {
	t.Helper()

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, args...)
	if environ != nil {
		cmd.Env = environ
	}
	return startCmdInPTY(t, ctx, cmd)
}

// runLstkInPTY runs the lstk binary inside a PTY so that ui.IsInteractive()
// returns true, making --non-interactive the actual condition under test.
func runLstkInPTY(t *testing.T, ctx context.Context, environ []string, args ...string) (string, error) {
	t.Helper()
	return startLstkInPTY(t, ctx, environ, args...).wait()
}

// write types s into the PTY (the child reads it as terminal input).
func (p *ptyProc) write(s string) {
	p.t.Helper()
	_, err := p.pt.Write([]byte(s))
	require.NoError(p.t, err)
}

// output returns the ANSI-stripped output captured so far.
func (p *ptyProc) output() string {
	return stripANSI(p.out.String())
}

// waitForOutput blocks until the captured output contains want.
func (p *ptyProc) waitForOutput(want string, msgAndArgs ...any) {
	p.t.Helper()
	p.waitForOutputTimeout(want, 10*time.Second, msgAndArgs...)
}

// waitForOutputTimeout is waitForOutput with a caller-chosen deadline, for
// prompts that only appear after slow work (e.g. a container becoming ready).
func (p *ptyProc) waitForOutputTimeout(want string, timeout time.Duration, msgAndArgs ...any) {
	p.t.Helper()
	require.Eventually(p.t, func() bool {
		return strings.Contains(p.output(), want)
	}, timeout, 100*time.Millisecond, msgAndArgs...)
}

// wait waits for the process to exit and the output to drain, then returns
// the trimmed, ANSI-stripped combined output and the wait error (an
// *exec.ExitError on non-zero exit, on every platform).
func (p *ptyProc) wait() (string, error) {
	p.t.Helper()
	err := xpty.WaitProcess(p.ctx, p.cmd)
	switch runtime.GOOS {
	case "windows":
		// ConPTY reads never return EOF on child exit; closing the pty
		// flushes pending output to the reader and then unblocks it.
		_ = p.pt.Close()
		<-p.done
	default:
		// EOF arrives when the last slave fd closes — normally right at
		// process exit. A grandchild that outlives the command (e.g. a
		// wrapped tool still running after the test killed lstk itself)
		// would keep the slave open forever, so bound the drain and then
		// force it by closing the master.
		select {
		case <-p.done:
		case <-time.After(5 * time.Second):
			_ = p.pt.Close()
			<-p.done
		}
	}
	return strings.TrimSpace(p.output()), err
}

// kill terminates the process and drains the PTY output — teardown for tests
// that assert on intermediate output rather than the exit status. Cancelling
// the exec context is not enough on Windows: ConPTY spawns bypass
// exec.CommandContext's kill-on-cancel machinery.
func (p *ptyProc) kill() {
	_ = p.cmd.Process.Kill()
	_, _ = p.wait()
}

var ansiEscape = regexp.MustCompile(`\x1b(\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// stripANSI removes ANSI escape sequences (colors, cursor movement, screen
// control) from PTY output. On unix these come from lstk itself; ConPTY
// additionally injects its own repaint sequences, so assertions must always
// run against the stripped form.
func stripANSI(raw string) string {
	return ansiEscape.ReplaceAllString(raw, "")
}

// ptyLines splits raw PTY output into display lines, dropping ANSI escape
// sequences and carriage returns so tests can assert on layout (blank lines,
// ordering) without depending on whether the terminal advertised colour.
func ptyLines(raw string) []string {
	stripped := strings.ReplaceAll(stripANSI(raw), "\r", "")
	lines := strings.Split(stripped, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return lines
}
