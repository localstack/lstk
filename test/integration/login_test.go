package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
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
			// Validate request payload
			var payload map[string]string
			err := json.NewDecoder(r.Body).Decode(&payload)
			require.NoError(t, err, "failed to decode auth request payload")
			assert.Equal(t, "lstk", payload["actor"], "actor should be lstk")
			assert.NotEmpty(t, payload["version"], "version should not be empty")

			w.WriteHeader(http.StatusCreated)
			err = json.NewEncoder(w).Encode(map[string]string{
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
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	cleanup()
	t.Cleanup(cleanup)

	// Require valid token from environment
	licenseToken := env.Require(t, env.AuthToken)

	// Create mock API server that returns the real token
	mockServer := createMockAPIServer(t, licenseToken, true)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "login")
	cmd.Env = env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	output := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, ptmx)
		close(outputCh)
	}()

	// Wait for browser prompt, then press Y to open browser
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("Open browser now?"))
	}, 10*time.Second, 100*time.Millisecond, "browser prompt should appear")
	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	// Wait for ENTER prompt, then press ENTER to confirm auth is complete
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("Waiting for authentication"))
	}, 10*time.Second, 100*time.Millisecond, "waiting prompt should appear")
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	// Wait for process to complete
	err = cmd.Wait()
	<-outputCh

	out := output.String()
	require.NoError(t, err, "login should succeed: %s", out)
	assert.Contains(t, out, "Verification code:")
	assert.Contains(t, out, "TEST123")
	assert.Contains(t, out, "Open browser now?")
	assert.Contains(t, out, "Checking if auth request is confirmed")
	assert.Contains(t, out, "Auth request confirmed")
	assert.Contains(t, out, "Fetching license token")
	assert.Contains(t, out, "Login successful")

	// Verify token was stored in keyring
	storedToken, err := GetAuthTokenFromKeyring()
	require.NoError(t, err)
	assert.Equal(t, licenseToken, storedToken)
}

func TestDeviceFlowFailure_RequestNotConfirmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockAPIServer(t, "", false)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "login")
	cmd.Env = env.Without(env.AuthToken).With(env.APIEndpoint, mockServer.URL)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	output := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, ptmx)
		close(outputCh)
	}()

	// Wait for browser prompt, then press Y to open browser
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("Open browser now?"))
	}, 10*time.Second, 100*time.Millisecond, "browser prompt should appear")
	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	// Wait for ENTER prompt, then press ENTER to confirm auth is complete
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("Waiting for authentication"))
	}, 10*time.Second, 100*time.Millisecond, "waiting prompt should appear")
	_, err = ptmx.Write([]byte("\r"))
	require.NoError(t, err)

	// Wait for process to complete
	err = cmd.Wait()
	<-outputCh

	out := output.String()
	require.Error(t, err, "expected login to fail when request not confirmed")
	assert.Contains(t, out, "Verification code:")
	assert.Contains(t, out, "Open browser now?")
	assert.Contains(t, out, "Waiting for authentication")
	assert.Contains(t, out, "Checking if auth request is confirmed")
	assert.Contains(t, out, "auth request not confirmed")

	// Verify no token was stored in keyring
	_, err = GetAuthTokenFromKeyring()
	assert.Error(t, err, "no token should be stored when login fails")
}
