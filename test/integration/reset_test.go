package integration_test

import (
	"bytes"
	"encoding/json"
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
	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
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
	must.NoError(t, err, "lstk reset failed: %s", stderr)
	must.Contains(t, stdout, "Emulator state reset")
	must.Eq(t, int32(1), calls.Load(), "reset endpoint should be called exactly once")
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
	must.Contains(t, stderr, "--force")
	must.Eq(t, int32(0), calls.Load(), "reset endpoint should not be called when confirmation is required")
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
	must.Contains(t, stdout, "not running")
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
	must.NotEmpty(t, stderr)
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
	must.NoError(t, err, "lstk reset failed: %s", stderr)
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
		must.NoError(t, err)

		cmd := exec.CommandContext(testContext(t), binPath, "reset")
		cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv))
		ptmx, err := pty.Start(cmd)
		must.NoError(t, err, "failed to start command in PTY")
		t.Cleanup(func() { _ = ptmx.Close() })

		out := &syncBuffer{}
		outputCh := make(chan struct{})
		go func() {
			_, _ = io.Copy(out, ptmx)
			close(outputCh)
		}()
		must.Eventually(t, func() bool {
			return bytes.Contains(out.Bytes(), []byte("Reset emulator state?"))
		}, 10*time.Second, 100*time.Millisecond, "confirmation prompt should appear")
		return ptmx, out, outputCh, cmd
	}

	t.Run("confirms with y", func(t *testing.T) {
		srv, calls := mockResetServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startReset(t, srv)
		_, err := ptmx.Write([]byte("y"))
		must.NoError(t, err)
		must.NoError(t, cmd.Wait())
		<-outputCh

		must.Contains(t, out.String(), "Emulator state reset")
		must.Eq(t, int32(1), calls.Load(), "reset endpoint should be called after confirmation")
	})

	t.Run("cancels with n", func(t *testing.T) {
		srv, calls := mockResetServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startReset(t, srv)
		_, err := ptmx.Write([]byte("n"))
		must.NoError(t, err)
		must.NoError(t, cmd.Wait())
		<-outputCh

		must.Contains(t, out.String(), "Cancelled")
		must.Eq(t, int32(0), calls.Load(), "reset endpoint must not be called when user cancels")
	})
}

func TestResetJSONSucceeds(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls := mockResetServer(t, http.StatusOK)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"reset", "--force", "--json",
	)
	must.NoError(t, err, "lstk reset --json failed: %s", stderr)
	requireExitCode(t, 0, err)
	must.Eq(t, int32(1), calls.Load(), "reset endpoint should be called exactly once")

	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "ok", envelope.Status)
	must.Eq(t, "reset", envelope.Command)

	var data struct {
		Emulator struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"emulator"`
		Reset bool `json:"reset"`
	}
	must.NoError(t, json.Unmarshal(envelope.Data, &data))
	must.Eq(t, "aws", data.Emulator.Type)
	must.True(t, data.Reset)
}

func TestResetJSONRequiresConfirmation(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Container required: the --force check runs after container discovery,
	// so without a running emulator the test would hit "not running" first.
	startTestContainer(t, ctx)

	stdout, _, err := runLstk(t, ctx, t.TempDir(), testEnvWithHome(t.TempDir(), ""), "reset", "--json")
	requireExitCode(t, 3, err)

	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "error", envelope.Status)
	must.NotNil(t, envelope.Error)
	must.Eq(t, "CONFIRMATION_REQUIRED", envelope.Error.Code)
	must.Eq(t, "USAGE", envelope.Error.Category)
	must.False(t, envelope.Error.Retryable)
}

func TestResetJSONNotConfigured(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	configFile := writeSnowflakeConfig(t, "4566")
	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		testEnvWithHome(t.TempDir(), ""), "--config", configFile, "reset", "--force", "--json",
	)
	requireExitCode(t, 1, err)

	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "error", envelope.Status)
	must.NotNil(t, envelope.Error)
	must.Eq(t, "EMULATOR_NOT_CONFIGURED", envelope.Error.Code)
	must.Eq(t, "EMULATOR", envelope.Error.Category)
}
