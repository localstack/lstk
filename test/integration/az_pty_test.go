package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// azureLikeHealthServer answers the endpoint probe the way the Azure emulator
// does: /_localstack/health without a "version" field, the version reachable
// only via /_localstack/info — the shape endpoint.Resolve classifies as Azure.
// Serving it lets these tests reach `lstk az`'s exec path via --endpoint-url,
// without Docker or a running emulator.
func azureLikeHealthServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_localstack/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": map[string]string{}})
		case "/_localstack/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.0.2"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeFakeStreamingAz creates a fake `az` standing in for a streaming Azure CLI
// command (`az webapp log tail`, `az container logs --follow`): it reports
// whether it was handed a terminal and what PYTHONUNBUFFERED it inherited,
// prints marker, then holds for holdFor before exiting (holdFor 0 exits at once).
//
// It is written in Python, not shell, so that it reproduces the actual defect
// rather than a proxy for it: the stall is CPython's stdout buffering, which
// engages only when stdout is not a terminal and PYTHONUNBUFFERED is unset —
// exactly the state `lstk az` used to hand the real, Python-based az. A shell
// fake writes straight through a pipe and so would stream even unfixed.
func writeFakeStreamingAz(t *testing.T, marker string, holdFor time.Duration) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("reproduces CPython's unix stdout buffering behind lstk's inner PTY, which proc.RunInPTY does not provide on Windows")
	}
	// Absolute interpreter path in the shebang: the test's PATH holds only this
	// fake az, so a `#!/usr/bin/env python3` line would not resolve.
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found on PATH; needed to reproduce the Azure CLI's stdout buffering")
	}
	dir := t.TempDir()
	// print() without flush=True on purpose — buffered output is the symptom.
	script := fmt.Sprintf(`#!%s
import os, sys, time
print("STDOUT_TTY:" + ("yes" if sys.stdout.isatty() else "no"))
print("PYTHONUNBUFFERED:" + os.environ.get("PYTHONUNBUFFERED", "<unset>"))
print(%q)
time.sleep(%f)
`, python, marker, holdFor.Seconds())
	require.NoError(t, os.WriteFile(filepath.Join(dir, "az"), []byte(script), 0755))
	return dir
}

// TestAzStreamsInPTY reproduces DEVX-1028, the `lstk az` twin of DEVX-1026:
// `lstk az` run on a terminal wraps the child's stdout in a spinner-stopping
// io.Writer, which makes os/exec hand the Azure CLI a pipe instead of the TTY.
// The Python-based az then block-buffers stdout, so a streaming command showed
// nothing until it exited. The fix runs the child on a PTY: this asserts the
// child sees a terminal, inherits PYTHONUNBUFFERED, and that its output reaches
// the user while it is still running.
func TestAzStreamsInPTY(t *testing.T) {
	t.Parallel()

	const marker = "streamed-while-running"
	// The fake holds far longer than the poll deadline, so seeing the marker can
	// only mean it arrived while the child was still running.
	const hold = 2 * time.Minute

	srv := azureLikeHealthServer(t)
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)
	fakeDir := writeFakeStreamingAz(t, marker, hold)

	e := append(env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()),
		unreachableDockerHost)

	ctx := testContext(t)
	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, "--endpoint-url", srv.URL, "az", "webapp", "log", "tail")
	cmd.Dir = workDir
	cmd.Env = e
	p := startCmdInPTY(t, ctx, cmd)

	// Poll generously: lipgloss queries the terminal for its background colour on
	// startup and nothing answers a test PTY, so lstk stalls for that query's
	// timeout (~5s) before it ever execs az. A real terminal answers at once.
	deadline := time.Now().Add(30 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		if strings.Contains(p.output(), marker) {
			seen = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.True(t, seen, "az produced no output while the child was still running — it was withheld in the CLI's stdout buffer until exit (DEVX-1028); output so far:\n%s", p.output())

	// The two mechanisms that keep the symptom above from returning, asserted
	// separately so a regression in either one is named rather than inferred.
	assert.Contains(t, p.output(), "STDOUT_TTY:yes", "the Azure CLI must be handed a terminal, not a pipe (DEVX-1028)")
	assert.Contains(t, p.output(), "PYTHONUNBUFFERED:1")

	// Ctrl-C via the PTY reaches the whole foreground process group (lstk and
	// the az child), matching how a user stops a streaming command.
	p.write("\x03")
	waited := make(chan struct{})
	go func() { _, _ = p.wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-waited
	}
}

// TestAzWithPipedStdoutKeepsPipe is the counterpart guard: with stdout
// redirected (`lstk az ... | grep`) the child must keep seeing a pipe, so a real
// az does not emit colors and CRLF into the pipeline.
func TestAzWithPipedStdoutKeepsPipe(t *testing.T) {
	t.Parallel()

	srv := azureLikeHealthServer(t)
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)
	fakeDir := writeFakeStreamingAz(t, "piped", 0)

	e := append(env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()),
		unreachableDockerHost)

	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "--endpoint-url", srv.URL, "az", "group", "list")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "STDOUT_TTY:no")
	assert.Contains(t, stdout, "piped")
}
