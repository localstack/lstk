package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPodListServer returns a test server that handles GET /_localstack/pods.
// pods is the list of cloud pod objects to return regardless of query params.
func mockPodListServer(t *testing.T, pods []map[string]any) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(map[string]any{"cloudpods": pods})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/pods" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockPodListServerCapture returns a test server that records the creator query param on each request.
func mockPodListServerCapture(t *testing.T, pods []map[string]any, creatorOut *string) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(map[string]any{"cloudpods": pods})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/pods" && r.Method == http.MethodGet {
			*creatorOut = r.URL.Query().Get("creator")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSnapshotListSuccess(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	var creator string
	srv := mockPodListServerCapture(t, []map[string]any{
		{"pod_name": "baseline-q2", "max_version": 3, "last_change": 1744727520},
		{"pod_name": "infra-2026-04", "max_version": 1, "last_change": nil},
	}, &creator)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	assert.Equal(t, "me", creator, "default list should send ?creator=me")
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "VERSION")
	assert.Contains(t, stdout, "LAST CHANGED")
	assert.Contains(t, stdout, "baseline-q2")
	assert.Contains(t, stdout, "infra-2026-04")
	assert.Contains(t, stdout, "3")
}

func TestSnapshotListAllFlag(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	var creator string
	srv := mockPodListServerCapture(t, []map[string]any{
		{"pod_name": "org-pod", "max_version": 1, "last_change": nil},
	}, &creator)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "list", "--all",
	)
	require.NoError(t, err, "lstk snapshot list --all failed: %s", stderr)
	assert.Equal(t, "", creator, "--all should omit ?creator param")
	assert.Contains(t, stdout, "org-pod")
}

func TestSnapshotListEmpty(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodListServer(t, []map[string]any{})

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	assert.Contains(t, stdout, "No snapshots found")
}

func TestSnapshotListEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Intentionally no startTestContainer: the emulator is not running.
	ctx := testContext(t)

	stdout, _, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")),
		"--non-interactive", "snapshot", "list",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not running")
}

func TestSnapshotListServerError(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"--non-interactive", "snapshot", "list",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, strings.ToLower(stderr), "error")
}

func TestSnapshotListWithAuthToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_localstack/pods" && r.Method == http.MethodGet {
			receivedAuth = r.Header.Get("Authorization")
			body, _ := json.Marshal(map[string]any{"cloudpods": []map[string]any{}})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runLstk(t, ctx, t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.LocalStackHost, lsHost(srv)).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	assert.NotEmpty(t, receivedAuth, "Authorization header should be sent when auth token is set")
}

func TestSnapshotListInteractive(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)
	srv := mockPodListServer(t, []map[string]any{
		{"pod_name": "my-pod", "max_version": 1, "last_change": 1744727520},
	})

	out, err := runLstkInPTY(t, ctx,
		env.Environ(testEnvWithHome(t.TempDir(), "")).With(env.LocalStackHost, lsHost(srv)),
		"snapshot", "list",
	)
	require.NoError(t, err, "interactive lstk snapshot list failed")
	assert.Contains(t, out, "my-pod")
}
