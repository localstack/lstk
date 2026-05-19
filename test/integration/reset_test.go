package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockResetServer returns a test server that records POST /_localstack/state/reset calls and replies with status.
func mockResetServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/state/reset" && r.Method == http.MethodPost {
			calls.Add(1)
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestResetSucceedsWithForce(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls := mockResetServer(t, http.StatusOK)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "reset", "--force",
	)
	require.NoError(t, err, "lstk reset failed: %s", stderr)
	assert.Contains(t, stdout, "Emulator state reset")
	assert.Equal(t, int32(1), calls.Load(), "reset endpoint should be called exactly once")
}

func TestResetFailsWithoutForceInNonInteractive(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Container required: the --force check runs after container discovery,
	// so without a running emulator the test would fail at "not running" first.
	startTestContainer(t, ctx)
	srv, calls := mockResetServer(t, http.StatusOK)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "reset",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "--force")
	assert.Equal(t, int32(0), calls.Load(), "reset endpoint should not be called when confirmation is required")
}

func TestResetLocalStackNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Intentionally no startTestContainer: the emulator is not running.

	stdout, _, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "reset", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
}

func TestResetReturnsErrorOnAPIFailure(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockResetServer(t, http.StatusInternalServerError)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "reset", "--force",
	)
	requireExitCode(t, 1, err)
	assert.NotEmpty(t, stderr)
}

func TestResetTelemetryEmitted(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockResetServer(t, http.StatusOK)

	analyticsSrv, events := mockAnalyticsServer(t)
	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "reset", "--force",
	)
	require.NoError(t, err, "lstk reset failed: %s", stderr)
	assertCommandTelemetry(t, events, "reset", 0)
}

func TestResetTelemetryOnFailure(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// No container running → "LocalStack is not running" failure.

	analyticsSrv, events := mockAnalyticsServer(t)
	_, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "reset", "--force",
	)
	requireExitCode(t, 1, err)
	assertCommandTelemetry(t, events, "reset", 1)
}

func TestResetInteractive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	startTestContainer(t, testContext(t))

	startReset := func(t *testing.T, srv *httptest.Server) (*os.File, *syncBuffer, chan struct{}, *exec.Cmd) {
		t.Helper()
		binPath, err := filepath.Abs(binaryPath())
		require.NoError(t, err)

		cmd := exec.CommandContext(testContext(t), binPath, "reset")
		cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv))
		ptmx, err := pty.Start(cmd)
		require.NoError(t, err, "failed to start command in PTY")
		t.Cleanup(func() { _ = ptmx.Close() })

		out := &syncBuffer{}
		outputCh := make(chan struct{})
		go func() {
			_, _ = io.Copy(out, ptmx)
			close(outputCh)
		}()
		require.Eventually(t, func() bool {
			return bytes.Contains(out.Bytes(), []byte("Reset emulator state?"))
		}, 10*time.Second, 100*time.Millisecond, "confirmation prompt should appear")
		return ptmx, out, outputCh, cmd
	}

	t.Run("confirms with y", func(t *testing.T) {
		srv, calls := mockResetServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startReset(t, srv)
		_, err := ptmx.Write([]byte("y"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "Emulator state reset")
		assert.Equal(t, int32(1), calls.Load(), "reset endpoint should be called after confirmation")
	})

	t.Run("cancels with n", func(t *testing.T) {
		srv, calls := mockResetServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startReset(t, srv)
		_, err := ptmx.Write([]byte("n"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "Cancelled")
		assert.Equal(t, int32(0), calls.Load(), "reset endpoint must not be called when user cancels")
	})
}
