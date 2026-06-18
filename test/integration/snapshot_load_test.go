package integration_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockLocalLoadServer returns a test server that handles local snapshot import:
//   - POST /_localstack/pods              → import (always succeeds)
//   - POST /_localstack/state/reset       → state reset (overwrite strategy)
//
// The returned function reports whether the reset endpoint was called.
func mockLocalLoadServer(t *testing.T) (*httptest.Server, func() bool) {
	t.Helper()
	var resetCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/_localstack/pods":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/_localstack/state/reset":
			resetCalled.Store(true)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, resetCalled.Load
}

// mockLocalLoadInvalidFileServer returns a test server whose import endpoint
// streams the emulator's BadZipFile error event, mimicking what the emulator
// returns when the source is not a valid snapshot archive.
func mockLocalLoadInvalidFileServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/_localstack/pods" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"error","message":"Invalid pod file: File is not a valid zip archive"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockPodLoadServer returns a test server that handles PUT /_localstack/pods/{name}.
// respondOK controls whether it emits a success or error completion event.
func mockPodLoadServer(t *testing.T, respondOK bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			if respondOK {
				_, _ = w.Write([]byte(`{"event":"service","service":"s3","status":"ok"}` + "\n"))
				_, _ = w.Write([]byte(`{"event":"service","service":"dynamodb","status":"ok"}` + "\n"))
				_, _ = w.Write([]byte(`{"event":"completion","status":"ok"}` + "\n"))
			} else {
				_, _ = w.Write([]byte(`{"event":"completion","status":"error","message":"platform unavailable"}` + "\n"))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// writeTestSnapFile creates a small file usable as a local snapshot source.
func writeTestSnapFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("SNAP"), 0600))
	return path
}

// --- no Docker required (parallel) ---

func TestSnapshotLoadRemoteRejected(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"s3://bucket/key", "oras://registry/image"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)
			_, stderr, err := runLstk(t, ctx, t.TempDir(),
				testEnvWithHome(t.TempDir(), ""),
				"--non-interactive", "snapshot", "load", ref,
			)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "not yet supported")
		})
	}
}

func TestSnapshotLoadPodNoAuthToken(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "authentication")
}

func TestSnapshotLoadPodInvalidName(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"pod:", "pod:-bad", "pod:_bad", "pod:my pod"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			ctx := testContext(t)
			_, stderr, err := runLstk(t, ctx, t.TempDir(),
				testEnvWithHome(t.TempDir(), ""),
				"--non-interactive", "snapshot", "load", ref,
			)
			requireExitCode(t, 1, err)
			assert.Contains(t, stderr, "invalid pod name")
		})
	}
}

func TestSnapshotLoadFileNotFound(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "load", "/no/such/snapshot.snapshot",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "snapshot file not found")
}

// --- Docker required ---

func TestSnapshotLoadLocalSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", snapPath,
	)
	require.NoError(t, err, "lstk snapshot load failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")
}

func TestSnapshotLoadLocalBareNameFallback(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	// Create snap.snapshot; pass bare name "snap" — ParseSource should resolve to snap.snapshot.
	writeTestSnapFile(t, dir, "snap.snapshot")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", filepath.Join(dir, "snap"),
	)
	require.NoError(t, err, "bare name fallback failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")
}

// TestSnapshotLoadLocalLegacyZipFallback verifies that snapshots saved as .zip by
// older lstk versions still load by bare name.
func TestSnapshotLoadLocalLegacyZipFallback(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	// Only a legacy snap.zip exists; pass bare name "snap" — ParseSource should still find it.
	writeTestSnapFile(t, dir, "snap.zip")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", filepath.Join(dir, "snap"),
	)
	require.NoError(t, err, "legacy .zip fallback failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")
}

func TestSnapshotLoadLocalInvalidFile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockLocalLoadInvalidFileServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", snapPath,
	)
	requireExitCode(t, 1, err)
	// The user-facing error is emitted through the sink (stdout); the underlying
	// "zip archive" detail must not leak to the user.
	assert.Contains(t, stdout, "not a valid snapshot")
	assert.NotContains(t, strings.ToLower(stdout+stderr), "zip")
}

func TestSnapshotLoadLocalOverwriteStrategy(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, wasReset := mockLocalLoadServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	_, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", "--merge=overwrite", snapPath,
	)
	require.NoError(t, err, "lstk snapshot load --merge=overwrite failed: %s", stderr)
	assert.True(t, wasReset(), "/_localstack/state/reset should have been called for overwrite strategy")
}

func TestSnapshotLoadPodSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodLoadServer(t, true)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load pod:my-baseline failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")
	assert.Contains(t, stdout, "my-baseline")
	assert.Contains(t, stdout, "s3")
	assert.Contains(t, stdout, "dynamodb")
}

func TestSnapshotLoadPodServerError(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodLoadServer(t, false)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "platform unavailable")
}

func TestSnapshotLoadTelemetryEmitted(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")
	analyticsSrv, events := mockAnalyticsServer(t)

	_, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "snapshot", "load", snapPath,
	)
	require.NoError(t, err, "lstk snapshot load failed: %s", stderr)
	assertCommandTelemetry(t, events, "snapshot load", 0)
}

func TestSnapshotLoadInteractive(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	out, err := runLstkInPTY(t, ctx,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"snapshot", "load", snapPath,
	)
	require.NoError(t, err, "interactive lstk snapshot load failed")
	assert.Contains(t, out, "Snapshot loaded")
}

func TestLoadAliasMatchesSnapshotLoad(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockLocalLoadServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AnalyticsEndpoint, analyticsSrv.URL),
		"--non-interactive", "load", snapPath,
	)
	require.NoError(t, err, "lstk load failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")

	// Alias must emit telemetry under the canonical name so usage isn't
	// split across "load" and "snapshot load" labels.
	assertCommandTelemetry(t, events, "snapshot load", 0)
}
