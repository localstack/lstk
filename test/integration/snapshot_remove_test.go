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
// status is the HTTP status code to respond with.
// The returned function reports how many times the endpoint was called.
func mockPodRemoveServer(t *testing.T, status int) (*httptest.Server, func() int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodDelete {
			calls.Add(1)
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, calls.Load
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

func TestSnapshotRemovePodNoAuthToken(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline", "--force",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "authentication")
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
	srv, calls := mockPodRemoveServer(t, http.StatusOK)

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

func TestSnapshotRemovePodServerError(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, calls := mockPodRemoveServer(t, http.StatusInternalServerError)

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
		srv, calls := mockPodRemoveServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startRemove(t, srv)
		_, err := ptmx.Write([]byte("y"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "deleted")
		assert.Equal(t, int32(1), calls(), "DELETE endpoint should be called after confirmation")
	})

	t.Run("cancels with n", func(t *testing.T) {
		srv, calls := mockPodRemoveServer(t, http.StatusOK)
		ptmx, out, outputCh, cmd := startRemove(t, srv)
		_, err := ptmx.Write([]byte("n"))
		require.NoError(t, err)
		require.NoError(t, cmd.Wait())
		<-outputCh

		assert.Contains(t, out.String(), "Cancelled")
		assert.Equal(t, int32(0), calls(), "DELETE endpoint must not be called when user cancels")
	})

	t.Run("force skips confirmation prompt", func(t *testing.T) {
		srv, calls := mockPodRemoveServer(t, http.StatusOK)

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
