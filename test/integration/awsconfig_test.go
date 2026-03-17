package integration_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awsConfigEnv returns a base environment with HOME set to an isolated temp
// directory, so tests never touch the real ~/.aws files.
func awsConfigEnv(t *testing.T) (env.Environ, string) {
	t.Helper()
	tmpHome := t.TempDir()
	// Runs before t.TempDir() cleanup (LIFO order). The emulator runs as root
	// inside the container, so files it writes into the volume are root-owned on
	// Linux. Go's TempDir cleanup can't delete them, so we use a Docker container
	// to remove them first.
	t.Cleanup(func() {
		volumeDir := filepath.Join(tmpHome, ".cache", "lstk", "volume")
		if _, err := os.Stat(volumeDir); err == nil {
			_ = exec.Command("docker", "run", "--rm", "-v", volumeDir+":/d", "alpine", "sh", "-c", "rm -rf /d/*").Run()
		}
	})
	e := env.With(env.AuthToken, env.Get(env.AuthToken)).With(env.Home, tmpHome)
	return e, tmpHome
}

func TestStartPromptsToCreateAWSProfileWhenMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	baseEnv, tmpHome := awsConfigEnv(t)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = baseEnv.With(env.APIEndpoint, mockServer.URL)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	// Wait for the AWS profile prompt.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Configure AWS profile in"))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile prompt should appear")

	// Press Y to confirm.
	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	// Wait for the success message.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("AWS profile successfully configured"))
	}, 10*time.Second, 200*time.Millisecond, "success message should appear")

	// Verify files were written to the isolated home dir, not the real one.
	configContent, err := os.ReadFile(filepath.Join(tmpHome, ".aws", "config"))
	require.NoError(t, err, "~/.aws/config should have been created")
	assert.Contains(t, string(configContent), "[profile localstack]")
	assert.Contains(t, string(configContent), "endpoint_url")

	credsContent, err := os.ReadFile(filepath.Join(tmpHome, ".aws", "credentials"))
	require.NoError(t, err, "~/.aws/credentials should have been created")
	normalizedCreds := strings.Join(strings.Fields(string(credsContent)), " ")
	assert.Contains(t, normalizedCreds, "[localstack]")
	assert.Contains(t, normalizedCreds, "aws_access_key_id = test")
	assert.Contains(t, normalizedCreds, "aws_secret_access_key = test")

	_ = cmd.Wait()
	<-outputCh
}

func TestStartSkipsAWSProfilePromptWhenAlreadyConfigured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	baseEnv, tmpHome := awsConfigEnv(t)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	// Pre-write a valid LocalStack AWS profile in the isolated home.
	awsDir := filepath.Join(tmpHome, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://127.0.0.1:4566\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0600))

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = baseEnv.With(env.APIEndpoint, mockServer.URL)

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	// Wait until the container is ready — that's the point at which post-start setup
	// runs, so if the prompt were going to appear it would already be in the output.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte(" ready"))
	}, 2*time.Minute, 200*time.Millisecond, "container should become ready")

	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	<-outputCh

	assert.NotContains(t, out.String(), "Configure AWS profile in",
		"profile prompt should not appear when profile is already correctly configured")
}

func TestStartNonInteractiveEmitsNoteWhenAWSProfileMissing(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	baseEnv, _ := awsConfigEnv(t)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	stdout, _, err := runLstk(t, testContext(t), "",
		baseEnv.With(env.APIEndpoint, mockServer.URL),
		"start",
	)
	require.NoError(t, err)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "No complete LocalStack AWS profile found")
}
