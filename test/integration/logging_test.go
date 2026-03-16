package integration_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createMockLicenseServerWithBody(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/license/request" {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestLogging_NonTTY_WritesToLogFile(t *testing.T) {
	requireDocker(t)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServerWithBody(http.StatusForbidden, `{"error":"subscription_expired","message":"Your subscription has expired"}`)
	defer mockServer.Close()

	logPath := filepath.Join(configDir(), "lstk.log")
	_ = os.Remove(logPath)

	ctx := testContext(t)
	_, _, err := runLstk(t, ctx, "", env.With(env.APIEndpoint, mockServer.URL).With(env.AuthToken, "test-token"), "start")
	require.Error(t, err, "expected lstk start to fail with forbidden license")

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[ERROR] license server response (HTTP 403)")
	assert.Contains(t, string(logContents), "subscription_expired")
}

func TestLogging_TTY_WritesToLogFile(t *testing.T) {
	requireDocker(t)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServerWithBody(http.StatusForbidden, `{"error":"subscription_expired","message":"Your subscription has expired"}`)
	defer mockServer.Close()

	logPath := filepath.Join(configDir(), "lstk.log")
	_ = os.Remove(logPath)

	ctx := testContext(t)
	_, err := runLstkInPTY(t, ctx, env.With(env.APIEndpoint, mockServer.URL).With(env.AuthToken, "test-token"), "start")
	require.Error(t, err, "expected lstk start to fail with forbidden license")

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[ERROR] license server response (HTTP 403)")
	assert.Contains(t, string(logContents), "subscription_expired")
}
