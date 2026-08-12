//go:build !windows

package integration_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

// These tests cover proc.Run's signal handling for wrapped tools end to end,
// using the reference extension's `signal-wait` mode: it exits 40 + the number
// of SIGINT/SIGTERM it received, so 41 proves exactly-once delivery, 40 proves
// the signal never arrived, and 42 proves a double signal (the terraform
// "two interrupts = abort immediately" hazard).

// startSignalWait starts `lstk sig signal-wait` with an isolated env and the
// reference extension on PATH, wiring stdio as configured by wire before start.
func startSignalWait(t *testing.T, wire func(*exec.Cmd)) *exec.Cmd {
	t.Helper()
	extDir := t.TempDir()
	installExtension(t, extDir, "sig")

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(testContext(t), binPath, "sig", "signal-wait")
	cmd.Dir = t.TempDir()
	cmd.Env = envWithPath(t.TempDir(), extDir)
	wire(cmd)
	return cmd
}

// waitForMarker polls out until the extension's readiness marker appears, so a
// signal is never sent before lstk has started the child (and armed its
// forwarding).
func waitForMarker(t *testing.T, out *syncBuffer) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "SIGNAL_WAIT_READY") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("extension never reported readiness; output so far:\n%s", out.String())
}

// drainPTY copies ptmx into a syncBuffer until the child exits (EIO on Linux /
// EOF on macOS), returning the buffer and a channel closed when copying ends.
func drainPTY(ptmx *os.File) (*syncBuffer, chan struct{}) {
	out := &syncBuffer{}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(done)
	}()
	return out, done
}

// TestWrappedToolReceivesForwardedSignalNonInteractive is the core DEVX-1000
// scenario: with no terminal attached (CI, supervisors), a SIGINT/SIGTERM sent
// to the lstk process alone must be forwarded to the wrapped tool — lstk itself
// swallows the signal (root context cancel) and, without forwarding, the tool
// would never learn it should shut down.
func TestWrappedToolReceivesForwardedSignalNonInteractive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sig  os.Signal
	}{
		{name: "SIGTERM", sig: syscall.SIGTERM},
		{name: "SIGINT", sig: os.Interrupt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := &syncBuffer{}
			cmd := startSignalWait(t, func(c *exec.Cmd) {
				c.Stdout = out
				c.Stderr = out
			})
			require.NoError(t, cmd.Start())

			waitForMarker(t, out)
			require.NoError(t, cmd.Process.Signal(tc.sig))

			err := cmd.Wait()
			requireExitCode(t, 41, err)
		})
	}
}

// TestWrappedToolReceivesForwardedSIGTERMInTerminal: SIGTERM is never generated
// by a terminal, so even when lstk runs interactively a `kill <lstk-pid>` (IDE
// stop button, `timeout 30 lstk ...`) must still be forwarded — the terminal's
// process group will not deliver it for us.
func TestWrappedToolReceivesForwardedSIGTERMInTerminal(t *testing.T) {
	t.Parallel()
	var ptmx *os.File
	cmd := startSignalWait(t, func(c *exec.Cmd) {})
	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	defer func() { _ = ptmx.Close() }()

	out, copied := drainPTY(ptmx)
	waitForMarker(t, out)
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))

	err = cmd.Wait()
	<-copied
	requireExitCode(t, 41, err)
}

// TestWrappedToolSingleSIGINTOnCtrlCInTerminal: interactively, Ctrl-C reaches
// the wrapped tool via the terminal's foreground process group; lstk must NOT
// forward a duplicate — a second, near-simultaneous SIGINT makes tools like
// terraform abort immediately instead of cleaning up.
func TestWrappedToolSingleSIGINTOnCtrlCInTerminal(t *testing.T) {
	t.Parallel()
	cmd := startSignalWait(t, func(c *exec.Cmd) {})
	ptmx, err := pty.Start(cmd)
	require.NoError(t, err)
	defer func() { _ = ptmx.Close() }()

	out, copied := drainPTY(ptmx)
	waitForMarker(t, out)
	_, err = ptmx.Write([]byte{0x03}) // Ctrl-C → SIGINT to the foreground process group
	require.NoError(t, err)

	err = cmd.Wait()
	<-copied
	requireExitCode(t, 41, err)
}

// TestWrappedToolSingleSIGINTOnCtrlCWithRedirectedStdin guards the piped-stdin
// regression: `yes | lstk terraform apply` (stdin redirected, stdout/stderr
// still the terminal) keeps lstk in the terminal's foreground process group, so
// Ctrl-C already reaches the wrapped tool via the group. Gating suppression on
// stdin alone would arm forwarding here and double-signal the tool.
func TestWrappedToolSingleSIGINTOnCtrlCWithRedirectedStdin(t *testing.T) {
	t.Parallel()
	ptmx, tty, err := pty.Open()
	require.NoError(t, err)
	defer func() { _ = ptmx.Close() }()

	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer func() { _ = devNull.Close() }()

	cmd := startSignalWait(t, func(c *exec.Cmd) {
		c.Stdin = devNull // stdin redirected: not a terminal
		c.Stdout = tty    // stdout/stderr: the terminal
		c.Stderr = tty
		// Make the PTY the controlling terminal so Ctrl-C on it signals the
		// foreground process group, exactly like a real interactive session.
		c.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 1}
	})
	require.NoError(t, cmd.Start())
	_ = tty.Close() // child holds its own copy of the slave side

	out, copied := drainPTY(ptmx)
	waitForMarker(t, out)
	_, err = ptmx.Write([]byte{0x03}) // Ctrl-C → SIGINT to the foreground process group
	require.NoError(t, err)

	err = cmd.Wait()
	<-copied
	requireExitCode(t, 41, err)
}
