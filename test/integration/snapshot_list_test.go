package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listCapture records what the platform list endpoint received. It is written from the
// httptest handler goroutine and read from the test body, so access is mutex-guarded.
type listCapture struct {
	mu      sync.Mutex
	called  bool
	creator string
	auth    string
}

func (c *listCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.called = true
	c.creator = r.URL.Query().Get("creator")
	c.auth = r.Header.Get("Authorization")
}

func (c *listCapture) get() (called bool, creator, auth string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.called, c.creator, c.auth
}

func mockCloudPodsServer(t *testing.T, pods []map[string]any, cap *listCapture) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(pods)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cloudpods" && r.Method == http.MethodGet {
			if cap != nil {
				cap.record(r)
			}
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

func listEnv(t *testing.T, srv *httptest.Server, token string) []string {
	t.Helper()
	return env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.APIEndpoint, srv.URL).
		With(env.AuthToken, token)
}

func TestSnapshotListSuccessWithoutDocker(t *testing.T) {
	t.Parallel()

	var cap listCapture
	srv := mockCloudPodsServer(t, []map[string]any{
		{"pod_name": "baseline-q2", "max_version": 3, "last_change": 1744727520},
		{"pod_name": "infra-2026-04", "max_version": 1, "last_change": nil},
	}, &cap)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	called, creator, _ := cap.get()
	require.True(t, called, "the platform list endpoint should have been called")
	assert.Equal(t, "me", creator, "default list should send ?creator=me")
	assert.Contains(t, stdout, "~ 2 snapshots")
	assert.Contains(t, stdout, "~ 2 snapshots\n\n  NAME")
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "VERSION")
	assert.Contains(t, stdout, "LAST CHANGED")
	assert.Contains(t, stdout, "baseline-q2")
	assert.Contains(t, stdout, "infra-2026-04")
	assert.Contains(t, stdout, "3")
}

func TestSnapshotListAllFlag(t *testing.T) {
	t.Parallel()

	var cap listCapture
	srv := mockCloudPodsServer(t, []map[string]any{
		{"pod_name": "org-pod", "max_version": 1, "last_change": nil},
	}, &cap)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "list", "--all",
	)
	require.NoError(t, err, "lstk snapshot list --all failed: %s", stderr)
	called, creator, _ := cap.get()
	require.True(t, called, "the platform list endpoint should have been called")
	assert.Equal(t, "", creator, "--all should omit the ?creator param")
	assert.Contains(t, stdout, "~ 1 snapshot")
	assert.Contains(t, stdout, "org-pod")
}

func TestSnapshotListEmpty(t *testing.T) {
	t.Parallel()

	srv := mockCloudPodsServer(t, []map[string]any{}, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	assert.Contains(t, stdout, "No snapshots found")
}

func TestSnapshotListSendsBasicAuthHeader(t *testing.T) {
	t.Parallel()

	var cap listCapture
	srv := mockCloudPodsServer(t, []map[string]any{}, &cap)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)
	_, _, auth := cap.get()
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(":test-token"))
	assert.Equal(t, expected, auth, "list should authenticate to the platform with Basic base64(\":\"+token)")
}

func TestSnapshotListRequiresAuthToken(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("platform must not be called without an auth token; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	environ := env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.APIEndpoint, srv.URL).
		Without(env.AuthToken)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		environ,
		"--non-interactive", "snapshot", "list",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Authentication required")
	assert.Contains(t, stdout, "lstk login")
}

func TestSnapshotListServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		env.Environ(testEnvWithHome(t.TempDir(), "")).
			With(env.APIEndpoint, srv.URL).
			With(env.AuthToken, "test-token"),
		"--non-interactive", "snapshot", "list",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, strings.ToLower(stderr), "error")
}

func TestSnapshotListInteractive(t *testing.T) {
	t.Parallel()

	srv := mockCloudPodsServer(t, []map[string]any{
		{"pod_name": "my-pod", "max_version": 1, "last_change": 1744727520},
	}, nil)

	out, err := runLstkInPTY(t, testContext(t),
		listEnv(t, srv, "test-token"),
		"snapshot", "list",
	)
	require.NoError(t, err, "interactive lstk snapshot list failed")
	assert.Contains(t, out, "my-pod")
}
