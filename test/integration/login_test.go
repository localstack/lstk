package integration_test

import (
	"context"
	"net/http"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// Browser flow tests

func TestBrowserFlowStoresToken(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = envWithoutAuthToken()

	// Keep stdin open so ENTER listener doesn't trigger immediately
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdinPipe.Close()

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

	// Login should succeed, but container will fail with invalid token
	assert.Contains(t, string(out), "Login successful")
	assert.Contains(t, string(out), "License activation failed")

	// Verify token was stored in keyring
	storedToken, err := keyring.Get(keyringService, keyringUser)
	require.NoError(t, err, "token should be stored in keyring")
	assert.Equal(t, "mock-token", storedToken)
}

// Device flow tests

func TestDeviceFlowShowsVerificationCode(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = envWithoutAuthToken()

	// Keep stdin open and get the pipe to simulate ENTER
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdinPipe.Close()

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
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("timeout waiting for command output")
	}
}
