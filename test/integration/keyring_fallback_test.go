package integration_test

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKeyringFallbackOnSystemFailure tests that when system keyring fails,
// the application falls back to encrypted file storage
func TestKeyringFallbackOnSystemFailure(t *testing.T) {
	// Skip on non-Linux since we can only reliably break the keyring on Linux
	if runtime.GOOS != "linux" {
		t.Skip("This test only runs on Linux where we can break the Secret Service backend")
	}

	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	// Require valid token from environment for real container start
	licenseToken := os.Getenv("LOCALSTACK_AUTH_TOKEN")
	require.NotEmpty(t, licenseToken, "LOCALSTACK_AUTH_TOKEN must be set to run this test")

	// Create mock API server that returns the real token
	mockServer := createMockAPIServer(t, licenseToken)
	defer mockServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	// Create environment with broken system keyring but file backend available
	env := envWithBrokenKeyring()
	env = append(env, "LOCALSTACK_API_ENDPOINT="+mockServer.URL)
	cmd.Env = env

	// Keep stdin open and get the pipe to simulate ENTER
	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdinPipe.Close()

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
		// Should complete device flow successfully even with broken system keyring
		assert.Contains(t, output, "Verification code:")
		assert.Contains(t, output, "Login successful")
		assert.NotContains(t, output, "License activation failed")

		// Critical test: Verify token was stored using file backend
		// (system keyring would have failed)
		storedToken, err := keyringGet(keyringService, keyringUser)
		require.NoError(t, err, "token should be stored via file backend fallback")
		assert.Equal(t, licenseToken, storedToken)

		// Verify container is running (proves token was retrieved from file backend)
		inspect, err := dockerClient.ContainerInspect(ctx, containerName)
		require.NoError(t, err)
		assert.True(t, inspect.State.Running, "container should be running")

	case <-ctx.Done():
		t.Fatal("test timed out")
	}
}

// envWithBrokenKeyring creates an environment where system keyring will fail
// but file backend can still work
func envWithBrokenKeyring() []string {
	env := envWithoutAuthToken()

	// On Linux, point to invalid D-Bus session to break Secret Service
	if runtime.GOOS == "linux" {
		env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path=/nonexistent/dbus")
	}

	return env
}
