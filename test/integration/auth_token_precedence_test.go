package integration_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedStoredAuthToken writes a stored credential into the file-based keyring of
// an isolated HOME (testEnvWithHome forces LSTK_KEYRING=file).
func seedStoredAuthToken(t *testing.T, home, token string) {
	t.Helper()
	configDir := expectedOSConfigDir(home, "")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "auth-token"), []byte(token), 0600))
}

func basicAuthHeader(token string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+token))
}

// LOCALSTACK_AUTH_TOKEN must win over stored credentials (DEVX-1023) so a
// per-invocation override takes effect without a `lstk logout` first.
func TestEnvAuthTokenOverridesStoredToken(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	seedStoredAuthToken(t, home, "stored-token")

	var cap listCapture
	srv := mockCloudPodsServer(t, []map[string]any{}, &cap)

	environ := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, srv.URL).
		With(env.AuthToken, "env-token")

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)

	called, _, auth := cap.get()
	require.True(t, called, "the platform list endpoint should have been called")
	assert.Equal(t, basicAuthHeader("env-token"), auth, "LOCALSTACK_AUTH_TOKEN should override the stored token")
}

func TestStoredTokenUsedWithoutEnvAuthToken(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	seedStoredAuthToken(t, home, "stored-token")

	var cap listCapture
	srv := mockCloudPodsServer(t, []map[string]any{}, &cap)

	environ := env.Environ(testEnvWithHome(home, "")).
		With(env.APIEndpoint, srv.URL).
		Without(env.AuthToken)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--non-interactive", "snapshot", "list",
	)
	require.NoError(t, err, "lstk snapshot list failed: %s", stderr)

	called, _, auth := cap.get()
	require.True(t, called, "the platform list endpoint should have been called")
	assert.Equal(t, basicAuthHeader("stored-token"), auth, "the stored token should be used when no env token is set")
}

func TestEnvAuthTokenOverridesStoredTokenForExternalEmulator(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	seedStoredAuthToken(t, home, "stored-token")

	authHeader := make(chan string, 1)
	health := awsHealthHandler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/_localstack/pods/my-baseline" {
			authHeader <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/x-ndjson")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"event":"completion","status":"ok"}` + "\n"))
			return
		}
		health.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	environ := env.Environ(testEnvWithHome(home, "")).
		With(env.AuthToken, "env-token").
		With(env.DisableEvents, "1")
	environ = append(environ, unreachableDockerHost)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ,
		"--non-interactive", "--endpoint-url", srv.URL, "snapshot", "load", "pod:my-baseline",
	)
	require.NoError(t, err, "lstk snapshot load failed: %s", stderr)

	select {
	case auth := <-authHeader:
		assert.Equal(t, basicAuthHeader("env-token"), auth, "LOCALSTACK_AUTH_TOKEN should override the stored token")
	default:
		t.Fatal("the external emulator pod endpoint should have been called")
	}
}
