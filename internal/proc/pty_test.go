package proc

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

func skipWithoutPTY(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
}

// syncWriter collects writes with timestamps so tests can assert on when
// output arrived, not just what arrived.
type syncWriter struct {
	mu     chan struct{}
	chunks []timedChunk
}

type timedChunk struct {
	at   time.Time
	data string
}

func newSyncWriter() *syncWriter {
	w := &syncWriter{mu: make(chan struct{}, 1)}
	w.mu <- struct{}{}
	return w
}

func (w *syncWriter) Write(p []byte) (int, error) {
	<-w.mu
	defer func() { w.mu <- struct{}{} }()
	w.chunks = append(w.chunks, timedChunk{at: time.Now(), data: string(p)})
	return len(p), nil
}

func (w *syncWriter) String() string {
	<-w.mu
	defer func() { w.mu <- struct{}{} }()
	var b strings.Builder
	for _, c := range w.chunks {
		b.WriteString(c.data)
	}
	return b.String()
}

func (w *syncWriter) firstWriteContaining(s string) (time.Time, bool) {
	<-w.mu
	defer func() { w.mu <- struct{}{} }()
	var seen strings.Builder
	for _, c := range w.chunks {
		seen.WriteString(c.data)
		if strings.Contains(seen.String(), s) {
			return c.at, true
		}
	}
	return time.Time{}, false
}

func TestRunInPTYChildSeesTerminal(t *testing.T) {
	skipWithoutPTY(t)
	out := newSyncWriter()

	cmd := exec.Command("sh", "-c", "test -t 1 && test -t 2 && echo is-a-tty")
	started, err := RunInPTY(cmd, out)

	require.True(t, started)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "is-a-tty")
}

func TestRunInPTYStreamsBeforeExit(t *testing.T) {
	skipWithoutPTY(t)
	out := newSyncWriter()

	start := time.Now()
	cmd := exec.Command("sh", "-c", "echo early-line; sleep 1")
	started, err := RunInPTY(cmd, out)
	elapsed := time.Since(start)

	require.True(t, started)
	require.NoError(t, err)
	require.GreaterOrEqual(t, elapsed, time.Second, "sanity: child slept after writing")
	at, ok := out.firstWriteContaining("early-line")
	require.True(t, ok, "output missing early-line: %q", out.String())
	assert.Less(t, at.Sub(start), 900*time.Millisecond,
		"output written before exit should reach the writer while the child still runs")
}

func TestRunInPTYPropagatesExitCode(t *testing.T) {
	skipWithoutPTY(t)
	out := newSyncWriter()

	cmd := exec.Command("sh", "-c", "exit 7")
	started, err := RunInPTY(cmd, out)

	require.True(t, started)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 7, exitErr.ExitCode())
}

// TestRunInPTYForwardsKeystrokesFromTerminalStdin reproduces DEVX-1049 at the
// package level: a pager spawned by the wrapped tool reads keyboard input from
// the terminal attached to its stderr — the inner PTY — so keystrokes typed on
// the user's terminal must be pumped into it. The child stands in for less by
// reading a line from fd 2, less's documented last-resort input source. The
// outer PTY pair stands in for the user's terminal, its slave being lstk's
// stdin.
func TestRunInPTYForwardsKeystrokesFromTerminalStdin(t *testing.T) {
	skipWithoutPTY(t)
	outerPtmx, outerTTY, err := pty.Open()
	require.NoError(t, err)
	defer func() { _ = outerPtmx.Close() }()
	defer func() { _ = outerTTY.Close() }()

	before, err := term.GetState(int(outerTTY.Fd()))
	require.NoError(t, err)

	out := newSyncWriter()
	cmd := exec.Command("sh", "-c", "echo page1; IFS= read -r key <&2; echo got:$key")
	cmd.Stdin = outerTTY

	done := make(chan error, 1)
	go func() {
		started, err := RunInPTY(cmd, out)
		if !started {
			err = exec.ErrNotFound // sentinel: must not happen on unix
		}
		done <- err
	}()

	require.Eventually(t, func() bool { return strings.Contains(out.String(), "page1") },
		5*time.Second, 10*time.Millisecond, "child never printed its first page")

	_, err = outerPtmx.WriteString("q\n")
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		t.Fatalf("keystroke typed on the user's terminal never reached the child reading its stderr terminal (DEVX-1049); output:\n%s", out.String())
	}
	assert.Contains(t, out.String(), "got:q")

	after, err := term.GetState(int(outerTTY.Fd()))
	require.NoError(t, err)
	assert.Equal(t, before, after, "the user's terminal must be restored from raw mode")
}

// TestRunInPTYStopsReadingTerminalAfterExit guards the input pump's teardown:
// once RunInPTY returns, nothing may still be reading the user's terminal. A
// pump left blocked in read would steal whatever the user types next from the
// caller (or a subsequent prompt) — the same stolen-keystroke shape as the
// old-less /dev/tty race in design.md. The pre-teardown pump deterministically
// fails this: it is already blocked in read when the post-return byte arrives.
func TestRunInPTYStopsReadingTerminalAfterExit(t *testing.T) {
	skipWithoutPTY(t)
	outerPtmx, outerTTY, err := pty.Open()
	require.NoError(t, err)
	defer func() { _ = outerPtmx.Close() }()
	defer func() { _ = outerTTY.Close() }()

	out := newSyncWriter()
	cmd := exec.Command("sh", "-c", "echo ready; IFS= read -r key <&2; echo got:$key")
	cmd.Stdin = outerTTY

	done := make(chan error, 1)
	go func() {
		_, err := RunInPTY(cmd, out)
		done <- err
	}()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "ready") },
		5*time.Second, 10*time.Millisecond)
	_, err = outerPtmx.WriteString("q\n")
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("RunInPTY did not return")
	}

	// The child is gone and RunInPTY has returned: a line typed now (the
	// restored terminal is canonical again, so it needs its newline) must be
	// readable from the terminal by its next legitimate reader, not consumed
	// by a leftover pump goroutine.
	_, err = outerPtmx.WriteString("x\n")
	require.NoError(t, err)
	readC := make(chan byte, 1)
	go func() {
		buf := make([]byte, 1)
		if n, err := outerTTY.Read(buf); err == nil && n == 1 {
			readC <- buf[0]
		}
	}()
	select {
	case b := <-readC:
		assert.Equal(t, byte('x'), b)
	case <-time.After(2 * time.Second):
		t.Fatal("byte typed after RunInPTY returned was consumed by a leftover input pump")
	}
}

// TestRunInPTYCtrlZIsANoOp pins the accepted ^Z behavior of the full-
// virtualization design (design.md, Evidence): the child runs as the leader
// of its own session on the inner PTY, so its process group is orphaned and
// the SIGTSTP raised by the PTY's line discipline is discarded per POSIX —
// the run continues, it just cannot be suspended. If this test starts
// failing, the suspension trade-off documented in the design has changed.
func TestRunInPTYCtrlZIsANoOp(t *testing.T) {
	skipWithoutPTY(t)
	outerPtmx, outerTTY, err := pty.Open()
	require.NoError(t, err)
	defer func() { _ = outerPtmx.Close() }()
	defer func() { _ = outerTTY.Close() }()

	out := newSyncWriter()
	cmd := exec.Command("sh", "-c", "echo ready; IFS= read -r key <&2; echo alive:$key")
	cmd.Stdin = outerTTY

	done := make(chan error, 1)
	go func() {
		_, err := RunInPTY(cmd, out)
		done <- err
	}()
	require.Eventually(t, func() bool { return strings.Contains(out.String(), "ready") },
		5*time.Second, 10*time.Millisecond)

	// ^Z (VSUSP) — the inner line discipline raises SIGTSTP, which the
	// orphaned child group discards. A suspended child would never answer.
	_, err = outerPtmx.WriteString("\x1a")
	require.NoError(t, err)
	_, err = outerPtmx.WriteString("go\n")
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		t.Fatalf("child stopped responding after ^Z — it was suspended, breaking the pinned no-op contract; output:\n%s", out.String())
	}
	assert.Contains(t, out.String(), "alive:go")
}

// TestRunInPTYPassesNonTerminalStdinThrough guards the redirected-stdin path
// (`yes | lstk aws ...`): piped data must reach the child as-is, never routed
// through the PTY's line discipline, and the child must keep seeing a
// non-terminal stdin.
func TestRunInPTYPassesNonTerminalStdinThrough(t *testing.T) {
	skipWithoutPTY(t)
	out := newSyncWriter()

	cmd := exec.Command("sh", "-c", "test -t 0 && echo stdin-is-tty || echo stdin-is-pipe; cat")
	cmd.Stdin = strings.NewReader("piped-data\n")
	started, err := RunInPTY(cmd, out)

	require.True(t, started)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "stdin-is-pipe")
	assert.Contains(t, out.String(), "piped-data")
}

func TestRunInPTYMergesStderrIntoOut(t *testing.T) {
	skipWithoutPTY(t)
	out := newSyncWriter()

	cmd := exec.Command("sh", "-c", "echo to-stdout; echo to-stderr >&2")
	started, err := RunInPTY(cmd, out)

	require.True(t, started)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "to-stdout")
	assert.Contains(t, out.String(), "to-stderr")
}
