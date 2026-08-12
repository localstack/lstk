package integration_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
)

// queryCapture records the query string of the pod load/diff request the emulator
// received, so tests can assert exactly what reached the wire.
type queryCapture struct {
	mu     sync.Mutex
	called bool
	query  url.Values
	path   string
}

func (c *queryCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.called = true
	c.query = r.URL.Query()
	c.path = r.URL.Path
}

func (c *queryCapture) get() (called bool, path string, query url.Values) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.called, c.path, c.query
}

// mockVersionedEmulator serves just enough of a LocalStack emulator for
// --endpoint-url to work (health/resources via awsHealthHandler) plus the pod
// load and diff endpoints. Using --endpoint-url keeps these tests Docker-free and
// parallel: with DOCKER_HOST broken, reaching the emulator at all proves the
// endpoint path was taken.
//
// loadCompletionMessage, when non-empty, makes the load stream fail with that
// message instead of succeeding.
func mockVersionedEmulator(t *testing.T, cap *queryCapture, loadCompletionMessage string) *httptest.Server {
	t.Helper()
	health := awsHealthHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/_localstack/pods/my-baseline":
			cap.record(r)
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			if loadCompletionMessage != "" {
				_, _ = w.Write([]byte(`{"event":"completion","status":"error","message":"` + loadCompletionMessage + `"}` + "\n"))
				return
			}
			_, _ = w.Write([]byte(`{"event":"service","service":"s3","status":"ok"}` + "\n"))
			_, _ = w.Write([]byte(`{"event":"completion","status":"ok"}` + "\n"))
		case r.Method == http.MethodGet && r.URL.Path == "/_localstack/pods/my-baseline/diff":
			cap.record(r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"s3":[{"operation_type":"ADDITION"}]}`))
		default:
			health.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func versionedLoadEnv(t *testing.T) []string {
	t.Helper()
	e := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.DisableEvents, "1").
		With(env.AuthToken, "test-token")
	return append(e, unreachableDockerHost)
}

// TestSnapshotLoadPodVersionReachesWire is the core assertion for version-pinned
// loads: "pod:<name>:<version>" must become ?version=N on the emulator's load
// endpoint. The query is read parsed rather than matched as a raw string, since
// url.Values.Encode sorts keys (merge before version).
func TestSnapshotLoadPodVersionReachesWire(t *testing.T) {
	t.Parallel()

	var cap queryCapture
	srv := mockVersionedEmulator(t, &cap, "")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", "pod:my-baseline:3",
	)
	must.NoError(t, err, "lstk snapshot load failed: %s", stderr)

	called, path, query := cap.get()
	must.True(t, called, "the pod load endpoint should have been called")
	must.Eq(t, "/_localstack/pods/my-baseline", path)
	must.Eq(t, "3", query.Get("version"))
	must.Eq(t, "account-region-merge", query.Get("merge"), "the default merge strategy still applies")

	must.Contains(t, stdout, "pod:my-baseline:3", "the loaded source should name the pinned version")
}

// TestSnapshotLoadPodWithoutVersionOmitsParam: an unpinned ref must not send
// ?version at all, so the emulator resolves the latest version itself.
func TestSnapshotLoadPodWithoutVersionOmitsParam(t *testing.T) {
	t.Parallel()

	var cap queryCapture
	srv := mockVersionedEmulator(t, &cap, "")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", "pod:my-baseline",
	)
	must.NoError(t, err, "lstk snapshot load failed: %s", stderr)

	called, _, query := cap.get()
	must.True(t, called, "the pod load endpoint should have been called")
	must.False(t, query.Has("version"), "an unpinned load must not send a version parameter")

	must.Contains(t, stdout, "pod:my-baseline")
	must.NotContains(t, stdout, "pod:my-baseline:")
}

// TestLoadAliasAcceptsPodVersion: the root `lstk load` alias shares the REF
// parser, so it gets version pinning without any flag of its own.
func TestLoadAliasAcceptsPodVersion(t *testing.T) {
	t.Parallel()

	var cap queryCapture
	srv := mockVersionedEmulator(t, &cap, "")

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "--endpoint-url", srv.URL, "load", "pod:my-baseline:2",
	)
	must.NoError(t, err, "lstk load failed: %s", stderr)

	called, _, query := cap.get()
	must.True(t, called, "the pod load endpoint should have been called")
	must.Eq(t, "2", query.Get("version"))
}

// TestSnapshotLoadPodVersionDryRun: --dry-run previews the same version the load
// would apply, so the version must reach the diff endpoint too.
func TestSnapshotLoadPodVersionDryRun(t *testing.T) {
	t.Parallel()

	var cap queryCapture
	srv := mockVersionedEmulator(t, &cap, "")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", "pod:my-baseline:3", "--dry-run",
	)
	must.NoError(t, err, "lstk snapshot load --dry-run failed: %s", stderr)

	called, path, query := cap.get()
	must.True(t, called, "the diff endpoint should have been called")
	must.Eq(t, "/_localstack/pods/my-baseline/diff", path)
	must.Eq(t, "3", query.Get("version"))

	must.Contains(t, stdout, "Dry-run results for pod:my-baseline:3")
	must.Contains(t, stdout, "No state was modified")
}

// TestSnapshotLoadPodVersionNotFound: the emulator's own message already names
// the highest available version, so it is surfaced as-is, together with the
// command that lists the valid ones.
func TestSnapshotLoadPodVersionNotFound(t *testing.T) {
	t.Parallel()

	const serverMsg = "Unable to load pod my-baseline with version 99. The maximum version available in the remote storage is 3"
	var cap queryCapture
	srv := mockVersionedEmulator(t, &cap, serverMsg)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", "pod:my-baseline:99",
	)
	requireExitCode(t, 1, err)

	_, _, query := cap.get()
	must.Eq(t, "99", query.Get("version"))

	must.Contains(t, stdout, "maximum version available in the remote storage is 3")
	must.Contains(t, stdout, "lstk snapshot versions pod:my-baseline")
}

// TestSnapshotLoadRejectsMalformedVersion: a colon in a ref is unambiguously a
// version separator, so a non-numeric suffix must be reported as a bad version —
// not as an "invalid pod name", which is what folding it into the name would give.
func TestSnapshotLoadRejectsMalformedVersion(t *testing.T) {
	t.Parallel()

	for _, ref := range []string{"pod:my-baseline:abc", "pod:my-baseline:0", "pod:my-baseline:"} {
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("the emulator must not be contacted for a malformed ref; got %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(srv.Close)

			_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
				"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", ref,
			)
			requireExitCode(t, 1, err)
			must.Contains(t, stderr, "invalid version")
			must.NotContains(t, stderr, "invalid pod name")
		})
	}
}

// TestSnapshotLoadS3RejectsPodVersion: S3 remotes have no version addressing, and
// the pod name there is a separate positional, so the rejection has to happen on
// that path too.
func TestSnapshotLoadS3RejectsPodVersion(t *testing.T) {
	t.Parallel()

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "snapshot", "load", "my-pod:3", "s3://bucket/prefix",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "S3 remotes do not support snapshot versions")
}

// TestSnapshotSaveRejectsPodVersion: the platform assigns a version on every
// save, so pinning a save destination is meaningless.
func TestSnapshotSaveRejectsPodVersion(t *testing.T) {
	t.Parallel()

	t.Run("pod destination", func(t *testing.T) {
		t.Parallel()
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
			"--non-interactive", "snapshot", "save", "pod:my-baseline:3",
		)
		requireExitCode(t, 1, err)
		must.Contains(t, stderr, "the platform assigns the version on each save")
		must.NotContains(t, stderr, "invalid pod name")
	})

	t.Run("s3 remote", func(t *testing.T) {
		t.Parallel()
		_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
			"--non-interactive", "snapshot", "save", "my-pod:3", "s3://bucket/prefix",
		)
		requireExitCode(t, 1, err)
		must.Contains(t, stderr, "S3 remotes do not support snapshot versions")
	})
}

// TestSnapshotRemoveRejectsPodVersion guards against the worst failure mode of a
// version-aware REF parser: remove addresses a whole pod, so a ref that parses
// but ignores its version would delete every version of it.
func TestSnapshotRemoveRejectsPodVersion(t *testing.T) {
	t.Parallel()

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "snapshot", "remove", "pod:my-baseline:3", "--force",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "drop the ':3'")
}

// TestStartSnapshotFlagAcceptsPodVersion: --snapshot goes through the same REF
// parser, so the auto-load path gets version pinning for free. The flag is
// validated eagerly, so a bad version fails before Docker is touched.
func TestStartSnapshotFlagAcceptsPodVersion(t *testing.T) {
	t.Parallel()

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), versionedLoadEnv(t),
		"--non-interactive", "start", "--snapshot", "pod:my-baseline:abc",
	)
	requireExitCode(t, 1, err)
	must.Contains(t, stderr, "invalid version")
}
