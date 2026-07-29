package integration_test

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for `lstk aws` that exercise the real aws CLI against a real
// LocalStack container (see localstack_test.go for the shared bring-up helpers)
// — unlike aws_cmd_test.go, which uses a fake aws binary and an alpine stand-in.
// They are gated on Docker + an aws binary + an auth token (CI installs the
// binaries and provides the token; otherwise they skip).

func requireRealAWSCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws binary not found on PATH")
	}
}

// TestAWSLogsTailFollowStreamsInPTY reproduces DEVX-1026: `lstk aws logs tail
// --follow` run on a terminal must stream log events as they are read, not sit
// silent until the process exits. lstk's interactive path wraps the child's
// stdout in a spinner-stopping io.Writer, which makes os/exec hand the aws CLI
// a pipe instead of the TTY; the Python-based aws CLI then block-buffers stdout
// (8 KB), so tail output only ever appeared on exit. The PTY here makes lstk
// take that interactive path; the test asserts an already-written event shows
// up while the follow process is still running.
func TestAWSLogsTailFollowStreamsInPTY(t *testing.T) {
	requireDocker(t)
	requireRealAWSCLI(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	e := env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())

	const logGroup = "/lstk-e2e/tail-follow"
	const marker = "hello-tail-follow"

	_, stderr, err := runLstk(t, ctx, "", e, "aws", "logs", "create-log-group", "--log-group-name", logGroup)
	require.NoError(t, err, "create-log-group failed: %s", stderr)
	_, stderr, err = runLstk(t, ctx, "", e, "aws", "logs", "create-log-stream", "--log-group-name", logGroup, "--log-stream-name", "s1")
	require.NoError(t, err, "create-log-stream failed: %s", stderr)
	events := fmt.Sprintf("timestamp=%d,message=%s", time.Now().UnixMilli(), marker)
	_, stderr, err = runLstk(t, ctx, "", e, "aws", "logs", "put-log-events", "--log-group-name", logGroup, "--log-stream-name", "s1", "--log-events", events)
	require.NoError(t, err, "put-log-events failed: %s", stderr)

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, binPath, "aws", "logs", "tail", logGroup, "--follow", "--since", "1h")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")

	out := &syncBuffer{}
	go func() { _, _ = io.Copy(out, ptmx) }()

	// The event already exists, so a streaming tail prints it on its first
	// poll — within a couple of seconds. Poll well past that so a slow first
	// CloudWatch call can't flake the test; the buggy behavior holds the line
	// in the aws CLI's stdout buffer until exit, which this deadline catches.
	deadline := time.Now().Add(30 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), marker) {
			seen = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Ctrl-C via the PTY reaches the whole foreground process group (lstk and
	// the aws child), matching what a user does to stop a follow.
	_, _ = ptmx.Write([]byte{3})
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-waitErr:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		<-waitErr
	}
	_ = ptmx.Close()

	require.True(t, seen,
		"tail --follow produced no output while running; event appeared only after exit (DEVX-1026). Output after exit:\n%s", out.String())
}
