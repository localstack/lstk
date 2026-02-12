package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockAPIServer creates a mock LocalStack API server for testing
func createMockAPIServer(t *testing.T, licenseToken string, confirmed bool) *httptest.Server {
	authReqID := "test-auth-req-id"
	exchangeToken := "test-exchange-token"
	bearerToken := "Bearer test-bearer-token"

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/auth/request":
			w.WriteHeader(http.StatusCreated)
			err := json.NewEncoder(w).Encode(map[string]string{
				"id":             authReqID,
				"code":           "TEST123",
				"exchange_token": exchangeToken,
			})
			require.NoError(t, err)

		case r.Method == "GET" && r.URL.Path == fmt.Sprintf("/v1/auth/request/%s", authReqID):
			w.WriteHeader(http.StatusOK)
			err := json.NewEncoder(w).Encode(map[string]bool{
				"confirmed": confirmed,
			})
			require.NoError(t, err)

		case r.Method == "POST" && r.URL.Path == fmt.Sprintf("/v1/auth/request/%s/exchange", authReqID):
			w.WriteHeader(http.StatusOK)
			err := json.NewEncoder(w).Encode(map[string]string{
				"id":         authReqID,
				"auth_token": bearerToken,
			})
			require.NoError(t, err)

		case r.Method == "GET" && r.URL.Path == "/v1/license/credentials":
			w.WriteHeader(http.StatusOK)
			err := json.NewEncoder(w).Encode(map[string]string{
				"token": licenseToken,
			})
			require.NoError(t, err)

		case r.Method == "POST" && r.URL.Path == "/v1/license/request":
			w.WriteHeader(http.StatusOK)

		default:
			t.Logf("Unhandled request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestDeviceFlowSuccess(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	// Require valid token from environment
	licenseToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	require.NotEmpty(t, licenseToken, "LOCALSTACK_AUTH_TOKEN must be set to run this test")

	// Create mock API server that returns the real token
	mockServer := createMockAPIServer(t, licenseToken, true)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "login")
	cmd.Env = append(
		envWithout("LOCALSTACK_AUTH_TOKEN"),
		"LOCALSTACK_API_ENDPOINT="+mockServer.URL,
	)

	// Keep stdin open and get the pipe to simulate ENTER
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer func() { _ = stdinPipe.Close() }()

	outputCh := make(chan []byte, 1)
	go func() {
		out, _ := cmd.CombinedOutput()
		outputCh <- out
	}()

	// Wait for device flow instructions
	time.Sleep(100 * time.Millisecond)

	// Simulate pressing ENTER to trigger device flow
	_, err = stdinPipe.Write([]byte("\n"))
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		output := string(out)
		// Should show device flow instructions
		assert.Contains(t, output, "Verification code:")
		assert.Contains(t, output, "TEST123")
		// Should complete device flow successfully
		assert.Contains(t, output, "Checking if auth request is confirmed")
		assert.Contains(t, output, "Auth request confirmed")
		assert.Contains(t, output, "Fetching license token")
		assert.Contains(t, output, "Login successful")

		// Verify token was stored in keyring
		storedToken, err := keyringGet(keyringService, keyringUser)
		require.NoError(t, err)
		assert.Equal(t, licenseToken, storedToken)

	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("timeout waiting for command output")
	}
}

func TestDeviceFlowFailure_RequestNotConfirmed(t *testing.T) {
	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockAPIServer(t, "", false)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "login")
	cmd.Env = append(
		envWithout("LOCALSTACK_AUTH_TOKEN"),
		"LOCALSTACK_API_ENDPOINT="+mockServer.URL,
	)

	// Keep stdin open and get the pipe to simulate ENTER
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer func() { _ = stdinPipe.Close() }()

	outputCh := make(chan []byte, 1)
	go func() {
		out, _ := cmd.CombinedOutput()
		outputCh <- out
	}()

	// Wait for device flow instructions to be printed
	time.Sleep(1 * time.Second)

	// Simulate pressing ENTER to trigger device flow
	_, err = stdinPipe.Write([]byte("\n"))
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		output := string(out)
		assert.Contains(t, output, "Verification code:")
		assert.Contains(t, output, "Waiting for authentication")
		assert.Contains(t, output, "Press ENTER when complete")
		// Should attempt device flow but fail because request not confirmed
		assert.Contains(t, output, "Checking if auth request is confirmed")
		assert.Contains(t, output, "auth request not confirmed")

		// Verify no token was stored in keyring
		_, err := keyringGet(keyringService, keyringUser)
		assert.Error(t, err, "no token should be stored when login fails")

	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("timeout waiting for command output")
	}
}
