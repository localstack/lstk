package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserFlowStoresToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

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
	)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	output := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(output, ptmx)
		close(outputCh)
	}()

	// Wait for verification code to appear (callback server should be ready)
	require.Eventually(t, func() bool {
		return bytes.Contains(output.Bytes(), []byte("TEST123"))
	}, 10*time.Second, 100*time.Millisecond, "verification code should appear")

	// Simulate browser callback with mock token
	var resp *http.Response
	require.Eventually(t, func() bool {
		var err error
		resp, err = http.Get("http://127.0.0.1:45678/auth/success?token=mock-token")
		return err == nil
	}, 5*time.Second, 100*time.Millisecond, "callback server should be ready")
	require.NoError(t, resp.Body.Close())

	// Wait for process to complete
	err = cmd.Wait()
	<-outputCh

	out := output.String()

	// Login should succeed
	require.NoError(t, err, "login should succeed via browser callback: %s", out)
	assert.Contains(t, out, "Login successful")

	// Verify token was stored in keyring
	storedToken, err := GetAuthTokenFromKeyring()
	require.NoError(t, err, "token should be stored in keyring")
	assert.Equal(t, "mock-token", storedToken)
}
