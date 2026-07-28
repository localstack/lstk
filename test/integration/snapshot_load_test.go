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

// mockPodDiffServer returns a test server that handles GET /_localstack/pods/{name}/diff.
// It responds with a fixed diff payload: two S3 additions and one DynamoDB modification.
func mockPodDiffServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") &&
			strings.HasSuffix(r.URL.Path, "/diff") &&
			r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"s3":[{"operation_type":"ADDITION"},{"operation_type":"ADDITION"}],"dynamodb":[{"operation_type":"MODIFICATION"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockPodDiffServerCapturingAuth behaves like mockPodDiffServer but records the
// Authorization header it received (empty when the header was absent).
func mockPodDiffServerCapturingAuth(t *testing.T) (*httptest.Server, func() string) {
	t.Helper()
	var gotAuth atomic.Value
	gotAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") &&
			strings.HasSuffix(r.URL.Path, "/diff") &&
			r.Method == http.MethodGet {
			gotAuth.Store(r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"s3":[{"operation_type":"ADDITION"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { return gotAuth.Load().(string) }
}

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

// mockPodLoadServerCapturingAuth returns a test server that handles
// PUT /_localstack/pods/{name} with the given status and records the
// Authorization header it received (empty when the header was absent). A status
// of 401/403 mimics an emulator that has no usable identity of its own.
func mockPodLoadServerCapturingAuth(t *testing.T, status int) (*httptest.Server, func() string) {
	t.Helper()
	var gotAuth atomic.Value
	gotAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/_localstack/pods/") || r.Method != http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth.Store(r.Header.Get("Authorization"))
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("no credentials configured"))
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event":"service","service":"s3","status":"ok"}` + "\n"))
		_, _ = w.Write([]byte(`{"event":"completion","status":"ok"}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() string { return gotAuth.Load().(string) }
}

// mockPodNotFoundServer mimics the emulator response when the requested cloud
// snapshot does not exist: the platform version lookup fails, so the load
// completes with the generic "Failed to get version information" diagnostic.
func mockPodNotFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") && r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"event":"completion","status":"error","message":"Failed to get version information from platform.. aborting"}` + "\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockPodDiffNotFoundServer mimics the emulator response when the requested
// cloud snapshot does not exist: the platform version lookup fails, so the
// diff request completes with the generic "Failed to get version information"
// diagnostic (same underlying message as mockPodNotFoundServer, but returned
// synchronously with an error status rather than via an NDJSON completion event).
func mockPodDiffNotFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/_localstack/pods/") &&
			strings.HasSuffix(r.URL.Path, "/diff") &&
			r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Error: Failed to get version information from platform.. aborting"))
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
	for _, ref := range []string{"oras://registry/image"} {
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

// Loading from S3 requires a pod name (the snapshot's identity within the
// bucket); the s3:// location alone is not enough.
func TestSnapshotLoadS3RequiresPodName(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "load", "s3://bucket/key",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "pod name is required")
}

func TestSnapshotLoadPodInvalidName(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{"pod:", "pod:bad.name", "pod:my pod"} {
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

// TestSnapshotLoadPathIsDirectory ensures passing a directory instead of a
// snapshot file fails early with a clear message, instead of surfacing a
// confusing HTTP-layer error once the directory reaches the upload code path.
func TestSnapshotLoadPathIsDirectory(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	dir := t.TempDir()
	target := filepath.Join(dir, "some-directory")
	require.NoError(t, os.Mkdir(target, 0o755))

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "load", target,
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "is a directory")
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

// A pod load must not be rejected client-side just because the caller supplied
// no token: the emulator was started with an identity (e.g. a job-level
// LOCALSTACK_AUTH_TOKEN that only `lstk start` saw) and reuses it, so lstk sends
// no Authorization header and lets the emulator decide.
func TestSnapshotLoadPodReusesEmulatorIdentity(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, gotAuth := mockPodLoadServerCapturingAuth(t, http.StatusOK)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load pod:my-baseline failed: %s", stderr)
	assert.Contains(t, stdout, "Snapshot loaded")
	assert.Empty(t, gotAuth(), "no Authorization header should be sent so the emulator reuses its own identity")
}

// An explicitly supplied token still overrides the emulator's identity.
func TestSnapshotLoadPodExplicitTokenOverridesEmulatorIdentity(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, gotAuth := mockPodLoadServerCapturingAuth(t, http.StatusOK)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "the-token"),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load pod:my-baseline failed: %s", stderr)
	assert.Equal(t, "Basic OnRoZS10b2tlbg==", gotAuth()) // base64(":the-token")
}

// When neither the caller nor the emulator has an identity, the emulator's
// rejection is surfaced as an actionable authentication error.
func TestSnapshotLoadPodEmulatorRejectsUnauthenticated(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, _ := mockPodLoadServerCapturingAuth(t, http.StatusUnauthorized)

	stdout, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Authentication failed")
	assert.Contains(t, stdout, "no credentials configured", "the emulator's own explanation should be surfaced")
	assert.Contains(t, stdout, "lstk login")
}

// With no emulator running there is no identity to reuse, so the auto-start path
// still requires a token from the environment or keychain.
func TestSnapshotLoadPodNoAuthTokenAndNoEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Intentionally no startTestContainer: load auto-starts the emulator, which
	// needs a token of its own.

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "authentication required")
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

// TestSnapshotLoadPodNotFound covers a non-existent cloud snapshot. The emulator
// cannot fetch version information for an unknown pod and reports the generic
// platform diagnostic; lstk must translate it into a clear "not found" message
// rather than leaking "Failed to get version information from platform".
func TestSnapshotLoadPodNotFound(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodNotFoundServer(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "load", "pod:does-not-exist",
	)
	requireExitCode(t, 1, err)
	// The user-facing error is emitted through the sink (stdout); the raw
	// platform diagnostic must not leak to the user.
	assert.Contains(t, stdout, "not found on the LocalStack platform")
	assert.NotContains(t, strings.ToLower(stdout+stderr), "version information")
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

// --- dry-run tests ---

func TestSnapshotLoadDryRunOnLocalRef(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)
	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.zip")

	_, stderr, err := runLstk(t, ctx, dir,
		testEnvWithHome(t.TempDir(), ""),
		"--non-interactive", "snapshot", "load", "--dry-run", snapPath,
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, "pod refs")
}

// A dry run without a token is not rejected client-side either; it needs a
// running emulator whose identity it can reuse (there is no auto-start).
func TestSnapshotLoadDryRunPodNoAuthToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	// Intentionally no startTestContainer: --dry-run does not auto-start.

	stdout, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "--dry-run", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
}

// The dry run reuses the running emulator's identity: no token supplied, no
// Authorization header sent, and the diff still runs.
func TestSnapshotLoadDryRunPodReusesEmulatorIdentity(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv, gotAuth := mockPodDiffServerCapturingAuth(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			Without(env.AuthToken),
		"--non-interactive", "snapshot", "load", "--dry-run", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load --dry-run failed: %s", stderr)
	assert.Contains(t, stdout, "Dry-run results")
	assert.Empty(t, gotAuth(), "no Authorization header should be sent so the emulator reuses its own identity")
}

func TestSnapshotLoadDryRunPodSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodDiffServer(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "load", "--dry-run", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load --dry-run failed: %s", stderr)
	assert.Contains(t, stdout, "Dry-run results")
	assert.Contains(t, stdout, "my-baseline")
	assert.Contains(t, stdout, "No state was modified.")
}

// TestSnapshotLoadDryRunPodNotFound covers a non-existent cloud snapshot, mirroring
// TestSnapshotLoadPodNotFound for the --dry-run path: the emulator's diff endpoint
// reports the same generic platform version-lookup failure for an unknown pod, and
// lstk must translate it into a clear "not found" message rather than leaking the
// raw platform diagnostic.
func TestSnapshotLoadDryRunPodNotFound(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodDiffNotFoundServer(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "load", "--dry-run", "pod:does-not-exist",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not found on the LocalStack platform")
	assert.NotContains(t, strings.ToLower(stdout+stderr), "version information")
}

// mockUnlicensedPodsServer mimics an emulator whose license does not include
// Cloud Pods: the plugin never loads, so its routes are never registered and
// every request falls through to the router's bare 404 with an empty body.
func mockUnlicensedPodsServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSnapshotLoadFeatureUnavailable reproduces DEVX-1009: on a plan without
// Cloud Pods, `lstk snapshot load <file>` used to fail with the raw, meaningless
// "LocalStack returned status 404: ". It must instead explain that snapshots need
// a paid plan and point at pricing.
func TestSnapshotLoadFeatureUnavailable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockUnlicensedPodsServer(t)

	dir := t.TempDir()
	snapPath := writeTestSnapFile(t, dir, "snap.snapshot")

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "load", snapPath,
	)
	requireExitCode(t, 1, err)
	// The user-facing error is emitted through the sink (stdout).
	assert.Contains(t, stdout, "Snapshots require a paid LocalStack plan")
	assert.Contains(t, stdout, "https://www.localstack.cloud/pricing")
	assert.NotContains(t, stdout+stderr, "status 404", "the raw HTTP status must not leak to the user")
}

// The save path funnels through a different shared helper than load, so it needs
// its own coverage.
func TestSnapshotSaveFeatureUnavailable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockUnlicensedPodsServer(t)

	dir := t.TempDir()

	stdout, stderr, err := runLstk(t, ctx, dir,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "save", filepath.Join(dir, "out.snapshot"),
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Snapshots require a paid LocalStack plan")
	assert.Contains(t, stdout, "https://www.localstack.cloud/pricing")
	assert.NotContains(t, stdout+stderr, "status 404")
}
