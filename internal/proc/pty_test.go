package proc

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
