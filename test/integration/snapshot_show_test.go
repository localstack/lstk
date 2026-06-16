package integration_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// showCapture records what the single-pod platform endpoint received.
type showCapture struct {
	mu     sync.Mutex
	called bool
	path   string
	auth   string
}

func (c *showCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.called = true
	c.path = r.URL.Path
	c.auth = r.Header.Get("Authorization")
}

func (c *showCapture) get() (called bool, path, auth string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.called, c.path, c.auth
}

// mockCloudPodServer serves GET /v1/cloudpods/<name> with the given JSON body.
func mockCloudPodServer(t *testing.T, name, body string, cap *showCapture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/cloudpods/"+name && r.Method == http.MethodGet {
			if cap != nil {
				cap.record(r)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSnapshotShowSuccessWithoutDocker(t *testing.T) {
	t.Parallel()

	var cap showCapture
	body := `{
		"pod_name": "my-baseline",
		"max_version": 1,
		"versions": [{
			"version": 1,
			"localstack_version": "2026.03",
			"size": 49597645,
			"description": "Pre-refactor baseline",
			"created_at": 1776263520,
			"services": ["s3", "lambda", "dynamodb", "sqs"],
			"cloud_control_resources": "{\"AWS::S3::Bucket\":[{\"id\":\"a\"},{\"id\":\"b\"},{\"id\":\"c\"}],\"AWS::Lambda::Function\":[{\"id\":\"f1\"}]}"
		}]
	}`
	srv := mockCloudPodServer(t, "my-baseline", body, &cap)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot show failed: %s", stderr)

	called, path, _ := cap.get()
	require.True(t, called, "the single-pod endpoint should have been called")
	assert.Equal(t, "/v1/cloudpods/my-baseline", path)

	assert.Contains(t, stdout, "my-baseline")
	assert.Contains(t, stdout, "2026-04-15 14:32 UTC")
	assert.Contains(t, stdout, "47.3 MB")
	assert.Contains(t, stdout, "2026.03")
	assert.Contains(t, stdout, "Pre-refactor baseline")
	assert.Contains(t, stdout, "s3, lambda, dynamodb, sqs")
	assert.Contains(t, stdout, "Resources")
	assert.Contains(t, stdout, "3 buckets")
	assert.Contains(t, stdout, "1 function\n", "count of one should use the singular noun")
}

func TestSnapshotShowWithoutResources(t *testing.T) {
	t.Parallel()

	body := `{"pod_name": "bare", "max_version": 1,
		"versions": [{"version": 1, "localstack_version": "2026.03", "services": ["s3", "sqs"], "size": 2048}]}`
	srv := mockCloudPodServer(t, "bare", body, nil)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:bare",
	)
	require.NoError(t, err, "lstk snapshot show failed: %s", stderr)
	assert.Contains(t, stdout, "s3, sqs")
	assert.NotContains(t, stdout, "Resources", "Resources section must be omitted when no counts are available")
}

func TestSnapshotShowSendsBasicAuthHeader(t *testing.T) {
	t.Parallel()

	var cap showCapture
	body := `{"pod_name": "p", "max_version": 1, "versions": [{"version": 1}]}`
	srv := mockCloudPodServer(t, "p", body, &cap)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:p",
	)
	require.NoError(t, err, "lstk snapshot show failed: %s", stderr)
	_, _, auth := cap.get()
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(":test-token"))
	assert.Equal(t, expected, auth)
}

func TestSnapshotShowRejectsLocalPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("platform must not be called for a local path; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "./my-snapshot",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, strings.ToLower(stderr), "local")
}

func TestSnapshotShowNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		listEnv(t, srv, "test-token"),
		"--non-interactive", "snapshot", "show", "pod:missing",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "not found")
	assert.Contains(t, stdout, "lstk snapshot list")
}

func TestSnapshotShowRequiresAuthToken(t *testing.T) {
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
		"--non-interactive", "snapshot", "show", "pod:my-baseline",
	)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Authentication required")
	assert.Contains(t, stdout, "lstk login")
}
