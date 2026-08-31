package integration_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// writeFakePagingTool installs faketool as a fake CLI (aws or az) in pager
// mode, standing in for a tool paging long output through its default pager:
// it prints a first page, then reads keystrokes the way less does — from the
// terminal on its stderr, under `lstk aws`/`lstk az` lstk's inner PTY, not
// the user's terminal (since version 577 less resolves its keyboard via
// ttyname(2); macOS ships 668) — reporting each one it receives and quitting
// on "q" like the real pager. Spawning a real less instead would make the
// test's outcome depend on the host's less version: pre-577 ones (e.g. 551 on
// Ubuntu 20.04) read /dev/tty and never exhibited the bug.
func writeFakePagingTool(t *testing.T, tool, pageMarker string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("reproduces a pager reading keystrokes from lstk's inner PTY, which proc.RunInPTY does not provide on Windows")
	}
	return writeFakeTool(t, tool, fakeToolConfig{
		Stdout: []string{pageMarker},
		Pager:  true,
	})
}

// waitForPagerExit sends "q" to the pager via the outer PTY and requires the
// process to exit and to have reported the keystroke.
func waitForPagerExit(t *testing.T, p *ptyProc) {
	t.Helper()
	p.write("q")

	done := make(chan struct{})
	go func() { _, _ = p.wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// Kill directly rather than via p.kill(): a wait is already in
		// flight above, and concurrent cmd.Wait calls deadlock.
		_ = p.cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		t.Fatalf("`q` never reached the pager: it reads from the terminal attached to its stderr (lstk's inner PTY), which lstk does not feed keystrokes into (DEVX-1049); output:\n%s", p.output())
	}

	require.Contains(t, p.output(), "PAGER_GOT:q",
		"pager exited without reporting the keystroke it was sent")
}

// TestAWSPagerReceivesKeystrokes reproduces DEVX-1049: when the AWS CLI pages
// its output on a terminal, the pager reads keyboard input from the terminal
// attached to its stderr — under `lstk aws` that is the inner PTY, whose
// master side lstk only ever read. Keystrokes stayed unread on the user's
// terminal, so SPACE/ENTER/q all did nothing and the command looked frozen.
// The fix feeds the user's keystrokes through the inner PTY; this asserts a
// "q" typed on the user's terminal reaches the pager and ends the command.
func TestAWSPagerReceivesKeystrokes(t *testing.T) {
	t.Parallel()

	const marker = "first-page-of-output"

	srv := awsHealthServer(t)
	fakeDir := writeFakePagingTool(t, "aws", marker)

	e := append(env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()),
		unreachableDockerHost)

	ctx := testContext(t)
	p := startLstkInPTY(t, ctx, e, "--endpoint-url", srv.URL, "aws", "dynamodb", "create-table")

	// Poll generously: lipgloss queries the terminal for its background colour on
	// startup and nothing answers a test PTY, so lstk stalls for that query's
	// timeout (~5s) before it ever execs aws. A real terminal answers at once.
	p.waitForOutputTimeout(marker, 30*time.Second,
		"fake aws never printed its first page — the pager path was not reached")

	waitForPagerExit(t, p)
}

// TestAzPagerReceivesKeystrokes is the `lstk az` twin of
// TestAWSPagerReceivesKeystrokes (DEVX-1049 names both): the az proxy wires
// its stdin separately from the aws one, so its interactive-input path is
// guarded separately too.
func TestAzPagerReceivesKeystrokes(t *testing.T) {
	t.Parallel()

	const marker = "first-page-of-output"

	srv := azureLikeHealthServer(t)
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)
	fakeDir := writeFakePagingTool(t, "az", marker)

	e := append(env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()),
		unreachableDockerHost)

	ctx := testContext(t)
	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, "--endpoint-url", srv.URL, "az", "storage", "account", "list")
	cmd.Dir = workDir
	cmd.Env = e
	p := startCmdInPTY(t, ctx, cmd)

	p.waitForOutputTimeout(marker, 30*time.Second,
		"fake az never printed its first page — the pager path was not reached")

	waitForPagerExit(t, p)
}
