package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSandboxCommandsUsePlatformAPI(t *testing.T) {
	t.Parallel()

	var resetMu sync.Mutex
	resetCalls := 0
	resetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/_localstack/state/reset" {
			resetMu.Lock()
			resetCalls++
			resetMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer resetSrv.Close()

	var mu sync.Mutex
	var createPayload map[string]any
	var deleted bool
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Basic OnRlc3QtdG9rZW4=", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/compute/instances":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createPayload))
			_, _ = w.Write([]byte(`{"instance_name":"dev","status":"pending","endpoint_url":"` + resetSrv.URL + `"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/compute/instances":
			_, _ = w.Write([]byte(`[{"instance_name":"dev","status":"running","endpoint_url":"` + resetSrv.URL + `","expiry_time":1893456000}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/compute/instances/dev":
			_, _ = w.Write([]byte(`{"instance_name":"dev","status":"running","endpoint_url":"` + resetSrv.URL + `"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/compute/instances/dev":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/compute/instances/dev/logs":
			_, _ = w.Write([]byte(`[{"content":"ready"},{"content":"serving"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer apiSrv.Close()

	e := sandboxTestEnv(t, apiSrv.URL)
	ctx := testContext(t)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "sandbox", "create", "dev", "--timeout", "90", "-e", "DEBUG=1")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, `"instance_name":"dev"`)
	mu.Lock()
	assert.Equal(t, "dev", createPayload["instance_name"])
	assert.Equal(t, float64(90), createPayload["lifetime"])
	assert.Equal(t, map[string]any{"DEBUG": "1"}, createPayload["env_vars"])
	mu.Unlock()

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "list")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "dev")
	assert.Contains(t, stdout, "running")

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "describe", "--name", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, `"status":"running"`)

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "url", "dev")
	require.NoError(t, err, stderr)
	assert.Equal(t, resetSrv.URL, stdout)

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "logs", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "ready")
	assert.Contains(t, stdout, "serving")

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "reset", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, `Reset sandbox instance "dev"`)
	resetMu.Lock()
	assert.Equal(t, 1, resetCalls)
	resetMu.Unlock()

	stdout, stderr, err = runLstk(t, ctx, t.TempDir(), e, "sandbox", "delete", "dev")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, `Deleted sandbox instance "dev"`)
	mu.Lock()
	assert.True(t, deleted)
	mu.Unlock()
}

func TestSandboxCreateRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	apiSrv := httptest.NewServer(http.NotFoundHandler())
	defer apiSrv.Close()

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), sandboxTestEnv(t, apiSrv.URL), "sandbox", "create", "dev", "-e", "DEBUG")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stderr, `invalid environment variable "DEBUG"`)
}

func sandboxTestEnv(t *testing.T, apiEndpoint string) []string {
	t.Helper()
	tmpHome := t.TempDir()
	xdgConfigHome := filepath.Join(tmpHome, "xdg-config-home")
	return env.Environ(testEnvWithHome(tmpHome, xdgConfigHome)).
		Without(env.AuthToken, env.APIEndpoint, env.DisableEvents).
		With(env.AuthToken, "test-token").
		With(env.APIEndpoint, apiEndpoint).
		With(env.DisableEvents, "1")
}
