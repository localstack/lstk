package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserFlowStoresToken(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	// Mock server that handles both auth and license endpoints
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/auth/request":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id":             "test-id",
				"code":           "TEST123",
				"exchange_token": "test-exchange",
			})
		case r.Method == "POST" && r.URL.Path == "/v1/license/request":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "login")
	cmd.Env = append(
		envWithout("LOCALSTACK_AUTH_TOKEN"),
		"LOCALSTACK_API_ENDPOINT="+mockServer.URL,
		"LSTK_FORCE_INTERACTIVE=1",
	)

	// Keep stdin open so ENTER listener doesn't trigger immediately
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer func() { _ = stdinPipe.Close() }()

	// Capture output asynchronously
	output := make(chan []byte)
	go func() {
		out, _ := cmd.CombinedOutput()
		output <- out
	}()

	// Wait for callback server to be ready
	time.Sleep(1 * time.Second)

	// Simulate browser callback with mock token
	resp, err := http.Get("http://127.0.0.1:45678/auth/success?token=mock-token")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	out := <-output

	// Login should succeed
	assert.Contains(t, string(out), "Login successful")

	// Verify token was stored in keyring
	storedToken, err := GetAuthTokenFromKeyring()
	require.NoError(t, err, "token should be stored in keyring")
	assert.Equal(t, "mock-token", storedToken)
}
