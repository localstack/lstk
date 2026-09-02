package integration_test

import (
	"context"
	"encoding/json"
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

// End-to-end tests for `lstk az` that exercise the *real* Azure CLI against a
// real LocalStack Azure emulator — unlike az_pty_test.go, which uses a fake az
// and a mock endpoint. They are gated on Docker + an az binary + an auth token
// (CI installs the binaries and provides the token; otherwise they skip), the
// same gating aws_e2e_test.go uses.
//
// Division of labour with az_pty_test.go: the fake-az test owns the DEVX-1028
// regression itself (it reproduces CPython's stdout buffering, which needs a
// child that keeps running after printing — no az command against the emulator
// does that). This suite owns the other half of the risk: that handing the real
// Azure CLI a PTY does not change what it produces. That risk is specific to the
// real binary, so a fake cannot cover it.

// hasInnerPTYLineEndings reports whether out came from a child that itself ran on
// a PTY, given that out was captured through an *outer* PTY (this test's own).
//
// A PTY translates the writer's "\n" to "\r\n" (ONLCR). With the child on its own
// PTY, lstk copies the already-translated "\r\n" to its stdout, and the outer PTY
// translates that trailing "\n" again — so a line ends "\r\r\n". Without the inner
// PTY, lstk writes plain "\n" and only the outer PTY translates: "\r\n". The
// doubled CR is therefore a direct, az-independent signature of the inner PTY.
//
// The signature is only valid while lstk's stdin is NOT a terminal: with stdin
// on the outer PTY, the DEVX-1049 input wiring switches that terminal to raw
// mode (OPOST off), the outer translation disappears, and inner-PTY output ends
// in a single "\r\n" instead.
func hasInnerPTYLineEndings(out string) bool {
	return strings.Contains(out, "\r\r\n")
}

// TestAzE2ERealCLIOnPTYPreservesOutput runs the real Azure CLI through `lstk az`
// on a terminal (the PTY path added for DEVX-1028) and on a pipe, asserting both
// produce the same usable result. `cloud show` is deliberately chosen over an ARM
// call: it round-trips through lstk's isolated AZURE_CONFIG_DIR, so it proves the
// real az honored the config lstk injected, without depending on which resource
// APIs the emulator implements.
func TestAzE2ERealCLIOnPTYPreservesOutput(t *testing.T) {
	requireDocker(t)
	requireAzCLI(t)
	token := requireAuthToken(t)
	if runtime.GOOS == "windows" {
		t.Skip("lstk runs wrapped tools on a Unix PTY only (proc.RunInPTY falls back to pipes on Windows), so the inner-PTY signature this test asserts never appears there")
	}

	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	tmpHome := t.TempDir()
	// The emulator runs as root and writes root-owned files into the lstk volume
	// dir; Go's TempDir cleanup can't remove those without help.
	t.Cleanup(func() {
		volumeDir := filepath.Join(tmpHome, ".cache", "lstk", "volume")
		if _, err := os.Stat(volumeDir); err == nil {
			_ = exec.Command("docker", "run", "--rm", "-v", volumeDir+":/d", "alpine", "sh", "-c", "rm -rf /d/*").Run()
		}
	})

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	baseEnv := env.With(env.DisableEvents, "1").WithHome(tmpHome).
		With(env.AuthToken, token).With(env.APIEndpoint, mockServer.URL)
	workDir := azureWorkDir(t)
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, workDir, baseEnv, "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	// Registers the LocalStack cloud in an isolated AZURE_CONFIG_DIR and writes
	// the setup marker `lstk az` requires. Non-interactive because runLstk is not
	// on a terminal.
	_, stderr, err = runLstk(t, ctx, workDir, baseEnv, "setup", "azure")
	require.NoError(t, err, "lstk setup azure failed: %s", stderr)

	// Pipe path: no PTY, so no CR at all, and the JSON is byte-clean.
	pipedOut, stderr, err := runLstk(t, ctx, workDir, baseEnv, "az", "cloud", "show", "-o", "json")
	require.NoError(t, err, "lstk az cloud show failed: %s", stderr)
	assert.NotContains(t, pipedOut, "\r", "a redirected stdout must stay a pipe, so the Azure CLI emits bare LFs")
	assert.Equal(t, "LocalStack", cloudNameFromJSON(t, pipedOut))

	// Output-only PTY path: stdin redirected to /dev/null keeps the DEVX-1049
	// input wiring off, so the outer terminal stays cooked and the inner PTY
	// shows up as the doubled-CR signature — the real az ran on a terminal,
	// and its output is still valid JSON once the PTY's CRs are stripped.
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer func() { _ = devNull.Close() }()
	ptyOut := runLstkAzInPTY(t, ctx, workDir, baseEnv, devNull, "az", "cloud", "show", "-o", "json")
	assert.True(t, hasInnerPTYLineEndings(ptyOut),
		"the real Azure CLI must run on a PTY when lstk's stdout and stderr are terminals (DEVX-1028); got:\n%q", ptyOut)
	assert.Equal(t, "LocalStack", cloudNameFromJSON(t, ptyOut))

	// Interactive PTY path: with stdin on the terminal, DEVX-1049 switches the
	// user's terminal to raw mode, so the doubled CR cannot appear — the inner
	// PTY's single "\r\n" must, and the JSON must stay usable.
	interOut := runLstkAzInPTY(t, ctx, workDir, baseEnv, nil, "az", "cloud", "show", "-o", "json")
	assert.Contains(t, interOut, "\r\n",
		"inner-PTY output must carry the PTY's CRLF translation; got:\n%q", interOut)
	assert.False(t, hasInnerPTYLineEndings(interOut),
		"with interactive stdin the outer terminal is raw (DEVX-1049), so the doubled-CR signature must not appear; got:\n%q", interOut)
	assert.Equal(t, "LocalStack", cloudNameFromJSON(t, interOut))
}

// cloudNameFromJSON extracts .name from an `az cloud show -o json` payload,
// tolerating PTY carriage returns and any leading terminal-query bytes lstk's
// styling writes before the child's first output.
func cloudNameFromJSON(t *testing.T, out string) string {
	t.Helper()
	clean := strings.ReplaceAll(out, "\r", "")
	start := strings.Index(clean, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON object in az output:\n%q", out)

	var payload struct {
		Name string `json:"name"`
	}
	require.NoError(t, json.Unmarshal([]byte(clean[start:]), &payload),
		"az output is not valid JSON after stripping CRs:\n%q", clean[start:])
	return payload.Name
}

// runLstkAzInPTY runs lstk on a PTY and returns everything it wrote once it
// exits. It does not use runLstkInPTY because that helper takes no working
// directory, and `lstk az` resolves its emulator config project-locally.
// stdin overrides lstk's stdin (xpty only attaches the PTY slave to a nil
// cmd.Stdin); pass nil to leave stdin on the PTY.
func runLstkAzInPTY(t *testing.T, ctx context.Context, workDir string, environ []string, stdin *os.File, args ...string) string {
	t.Helper()
	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = workDir
	cmd.Env = environ
	if stdin != nil {
		cmd.Stdin = stdin
		// fd 0 is no longer the PTY slave, so the controlling-terminal ioctl
		// (Setctty inside startCmdInPTY) must target stdout instead.
		setCttyToStdout(cmd)
	}
	p := startCmdInPTY(t, ctx, cmd)

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := p.wait()
		done <- result{out, err}
	}()

	var r result
	select {
	case r = <-done:
	case <-time.After(2 * time.Minute):
		_ = p.cmd.Process.Kill()
		r = <-done
	}
	require.NoError(t, r.err, "lstk %v failed on a PTY; output:\n%s", args, r.out)
	return r.out
}
