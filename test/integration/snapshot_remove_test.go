package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPodRemoveServer returns a test server that handles DELETE /_localstack/pods/{name}.
// status is the HTTP status code to respond with. The returned functions report how
// many times the endpoint was called and which Authorization header it last received
// (empty when the header was absent).
func mockPodRemoveServer(t *testing.T, status int) (srv *httptest.Server, calls func() int32, auth func() string) {
	t.Helper()
	var callCount atomic.Int32
	var gotAuth atomic.Value
	gotAuth.Store("")
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodDelete {
			callCount.Add(1)
			gotAuth.Store(r.Header.Get("Authorization"))
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, callCount.Load, func() string { return gotAuth.Load().(string) }
}

// --- no Docker required (parallel) ---

func TestSnapshotRemoveLocalRefRejected(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "./my-baseline.zip",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "resolves to a local file")
	assert.Contains(t, stderr, "CLI cannot delete local files")
}

func TestSnapshotRemoveLocalBareNameRejected(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "resolves to a local file")
	assert.Contains(t, stderr, "CLI cannot delete local files")
}

// Removal goes through the emulator, so without a caller-supplied token there is
// no client-side rejection: with no emulator running the command fails on that
// instead.
func TestSnapshotRemovePodNoAuthTokenAndNoEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Intentionally no startTestContainer: the emulator is not running.

	stdout, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
}

func TestSnapshotRemovePodInvalidName(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"pod:", "pod:bad.name", "pod:my pod"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)

			_, stderr, err := runLstk(t, ctx, t.TempDir(),
				testEnvWithHome(t.TempDir(), ""),
				"--non-interactive", "snapshot", "remove", ref,
			)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "invalid pod name")
		})
	}
}

func TestSnapshotRemoveNonInteractiveRequiresForce(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "--force")
}

func TestSnapshotRemoteSchemeRejected(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"s3://bucket/snap", "oras://registry/snap"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)

			_, stderr, err := runLstk(t, ctx, t.TempDir(),
				testEnvWithHome(t.TempDir(), ""),
				"--non-interactive", "snapshot", "remove", ref,
			)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "not yet supported")
		})
	}
}

// --- Docker required ---

func TestSnapshotRemovePodSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls, _ := mockPodRemoveServer(t, http.StatusOK)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	require.NoError(t, err, "lstk snapshot remove pod:my-baseline failed: %s", stderr)
	assert.Contains(t, stdout, "my-baseline")
	assert.Contains(t, stdout, "deleted")
	assert.Equal(t, int32(1), calls(), "DELETE endpoint should be called exactly once")
}

// Removal without a caller-supplied token reuses the running emulator's
// identity: lstk sends no Authorization header instead of failing client-side.
func TestSnapshotRemovePodReusesEmulatorIdentity(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls, gotAuth := mockPodRemoveServer(t, http.StatusOK)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			Without(env.AuthToken),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	require.NoError(t, err, "lstk snapshot remove pod:my-baseline failed: %s", stderr)
	assert.Contains(t, stdout, "deleted")
	assert.Equal(t, int32(1), calls())
	assert.Empty(t, gotAuth(), "no Authorization header should be sent so the emulator reuses its own identity")
}

// A 401 from the emulator (neither side has an identity) is rendered as an
// actionable authentication error carrying the emulator's own explanation.
func TestSnapshotRemovePodEmulatorRejectsUnauthenticated(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _, _ := mockPodRemoveServer(t, http.StatusUnauthorized)

	stdout, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			Without(env.AuthToken),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Authentication failed")
	assert.Contains(t, stdout, "lstk login")
}

func TestSnapshotRemovePodServerError(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls, _ := mockPodRemoveServer(t, http.StatusInternalServerError)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "pod remove failed")
	assert.Equal(t, int32(1), calls(), "DELETE endpoint should be called even when server errors")
}

func TestSnapshotRemovePodNotFound(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Error: Cloud Pod 'my-snapshot' not found."))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "remove", "pod:my-snapshot", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, `"my-snapshot"`)
	assert.Contains(t, stderr, "not found")
	assert.NotContains(t, stderr, "HTTP 500")
	assert.NotContains(t, stderr, "pod remove failed")
}

func TestSnapshotRemoveInteractive(t *testing.T) {
	if testing.Short() {
		t.Skip("PTY test skipped in short mode")
	}
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	startTestContainer(t, testContext(t))

	startRemove := func(t *testing.T, srv *httptest.Server) (*os.File, *syncBuffer, chan struct{}, *exec.Cmd) {
		t.Helper()
		binPath, err := filepath.Abs(binaryPath())
		require.NoError(t, err)

		cmd := exec.CommandContext(testContext(t), binPath, "snapshot", "remove", "pod:my-baseline")
		cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token")
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
			return bytes.Contains(out.Bytes(), []byte("Delete cloud snapshot"))
		}, 10*time.Second, 100*time.Millisecond, "confirmation prompt should appear")
		return ptmx, out, outputCh, cmd
	}

	t.Run("confirms with y", func(t *testing.T) {
		srv, calls, _ := mockPodRemoveServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startRemove(t, srv)
		_, err := ptmx.Write([]byte("y"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "deleted")
		assert.Equal(t, int32(1), calls(), "DELETE endpoint should be called after confirmation")
	})

	t.Run("cancels with n", func(t *testing.T) {
		srv, calls, _ := mockPodRemoveServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startRemove(t, srv)
		_, err := ptmx.Write([]byte("n"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "Cancelled")
		assert.Equal(t, int32(0), calls(), "DELETE endpoint must not be called when user cancels")
	})

	t.Run("force skips confirmation prompt", func(t *testing.T) {
		srv, calls, _ := mockPodRemoveServer(t, http.StatusOK)

		binPath, err := filepath.Abs(binaryPath())
		require.NoError(t, err)
		cmd := exec.CommandContext(testContext(t), binPath, "snapshot", "remove", "pod:my-baseline", "--force")
		cmd.Env = env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token")
		ptmx, err := pty.Start(cmd)
		require.NoError(t, err, "failed to start command in PTY")
		t.Cleanup(func() { _ = ptmx.Close() })

		out := &syncBuffer{}
		outputCh := make(chan struct{})
		go func() {
			_, _ = io.Copy(out, ptmx)
			close(outputCh)
		}()

		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.NotContains(t, out.String(), "Delete cloud snapshot", "confirmation prompt must not appear with --force")
		assert.Contains(t, out.String(), "deleted")
		assert.Equal(t, int32(1), calls(), "DELETE endpoint should be called without confirmation")
	})
}
