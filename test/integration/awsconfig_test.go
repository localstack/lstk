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

// awsConfigEnv returns a base environment with the home directory set to an
// isolated temp directory, so tests never touch the real ~/.aws files. Both HOME
// (Unix) and USERPROFILE (Windows) are overridden because os.UserHomeDir — which
// awsconfig uses to locate ~/.aws — reads USERPROFILE, not HOME, on Windows.
//
// A minimal AWS-emulator config.toml is pre-written so `lstk start` doesn't hit
// the first-run emulator-selection prompt (config.toml already exists, so it's
// not a first run) — these tests are about the post-start AWS-profile flow, not
// emulator selection, which is covered separately in emulator_select_test.go.
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
	writeConfigFile(t, filepath.Join(tmpHome, ".config", "lstk", "config.toml"))
	e := env.With(env.AuthToken, env.Get(env.AuthToken)).With(env.Home, tmpHome).With(env.UserProfile, tmpHome)
	return e, tmpHome
}

func TestStartPromptsWhenAWSProfileMissingEverywhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

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

	// Wait for the prompt emitted after the container becomes ready.
	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte(awsSetupPrompt))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile prompt should appear")

	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	err = cmd.Wait()
	<-outputCh
	require.NoError(t, err, "lstk start should exit successfully")

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
}

func TestStartSkipsAWSProfilePromptWhenAlreadyConfigured(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

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
		return bytes.Contains(out.Bytes(), []byte("LocalStack is running"))
	}, 2*time.Minute, 200*time.Millisecond, "container should become ready")

	// Teardown only: lstk may already have exited on its own, so don't assert on Wait's error.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	<-outputCh

	assert.NotContains(t, out.String(), awsSetupPrompt,
		"profile prompt should not appear when profile is already correctly configured")
}

const awsSetupPrompt = "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?"

func TestStartNonInteractiveEmitsNoteWhenAWSProfileMissing(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

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
	assert.Contains(t, stdout, "LocalStack AWS profile is incomplete. Run 'lstk setup aws'.")
}

func TestStartEmitsNoteWhenAWSProfileIsPartial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	t.Cleanup(cleanup)

	baseEnv, tmpHome := awsConfigEnv(t)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	awsDir := filepath.Join(tmpHome, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
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

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("LocalStack AWS profile is incomplete. Run 'lstk setup aws'."))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile note should appear")

	err = cmd.Wait()
	<-outputCh
	require.NoError(t, err, "lstk start should exit successfully")

	assert.NotContains(t, out.String(), "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?",
		"profile prompt should not appear for a partial setup")
}

func TestSetupAWSCreatesAWSProfileWhenConfirmed(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	baseEnv, tmpHome := awsConfigEnv(t)

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "setup", "aws")
	cmd.Env = baseEnv

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
		return bytes.Contains(out.Bytes(), []byte(awsSetupPrompt))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile prompt should appear")

	// Press Y to confirm.
	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	err = cmd.Wait()
	<-outputCh
	require.NoError(t, err)

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

	assert.Contains(t, out.String(), "Created LocalStack profile in ~/.aws")
	assert.NotContains(t, out.String(), "Skipped adding LocalStack AWS profile.")
}

// TestSetupAWSExitsNonZeroWhenProfileWriteFails guards DEVX-941. Writing the
// profile is the whole purpose of `lstk setup aws`, but the command used to emit a
// warning and return nil when the write failed — exiting 0 and masking the failure
// from users, CI, and agents. We make ~/.aws read-only so CheckProfileStatus still
// sees the profile files as absent (the prompt appears) but the actual write fails,
// then confirm the prompt and assert a non-zero exit.
func TestSetupAWSExitsNonZeroWhenProfileWriteFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions, so the profile write would not fail")
	}
	baseEnv, tmpHome := awsConfigEnv(t)

	// A read-only ~/.aws keeps the profile files absent (so the prompt still appears)
	// while making their creation fail inside upsertSection's SaveTo.
	awsDir := filepath.Join(tmpHome, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0500))
	// Restore write permission before t.TempDir cleanup so the dir can be removed.
	t.Cleanup(func() { _ = os.Chmod(awsDir, 0700) })

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "setup", "aws")
	cmd.Env = baseEnv

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte(awsSetupPrompt))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile prompt should appear")

	_, err = ptmx.Write([]byte("y"))
	require.NoError(t, err)

	err = cmd.Wait()
	<-outputCh
	requireExitCode(t, 1, err)

	assert.Contains(t, out.String(), "Could not set up the LocalStack AWS profile")
	assert.NotContains(t, out.String(), "Created LocalStack profile")
}

func TestSetupAWSDoesNotCreateAWSProfileWhenDeclined(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	baseEnv, tmpHome := awsConfigEnv(t)

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "setup", "aws")
	cmd.Env = baseEnv

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start command in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte(awsSetupPrompt))
	}, 2*time.Minute, 200*time.Millisecond, "AWS profile prompt should appear")

	_, err = ptmx.Write([]byte("n"))
	require.NoError(t, err)

	err = cmd.Wait()
	<-outputCh
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpHome, ".aws", "config"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(tmpHome, ".aws", "credentials"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	assert.Contains(t, out.String(), "Skipped adding LocalStack AWS profile.")
	assert.NotContains(t, out.String(), "Created LocalStack profile in ~/.aws/config")
}

func TestSetupAWSNonInteractiveCreatesProfile(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	stdout, _, err := runLstk(t, testContext(t), "",
		baseEnv,
		"setup", "aws",
	)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Created LocalStack profile in ~/.aws")
	assert.NotContains(t, stdout, "requires an interactive terminal")

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
}

func TestSetupAWSNonInteractiveIsIdempotent(t *testing.T) {
	t.Parallel()
	baseEnv, _ := awsConfigEnv(t)

	// First run writes the profile.
	_, _, err := runLstk(t, testContext(t), "", baseEnv, "setup", "aws")
	requireExitCode(t, 0, err)

	// Second run sees an already-correct profile and is a no-op success — it must
	// not be treated as an overwrite (no --force required).
	stdout, _, err := runLstk(t, testContext(t), "", baseEnv, "setup", "aws")
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "already configured")
}

func TestSetupAWSNonInteractiveOverwriteRequiresForce(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	// Pre-seed a localstack profile with different values.
	awsDir := filepath.Join(tmpHome, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://example.com:9999\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = WRONG\naws_secret_access_key = WRONG\n"), 0600))

	stdout, _, err := runLstk(t, testContext(t), "", baseEnv, "setup", "aws")
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "--force")

	// The existing profile must be left untouched.
	configContent, err := os.ReadFile(filepath.Join(awsDir, "config"))
	require.NoError(t, err)
	assert.Contains(t, string(configContent), "example.com:9999", "config must not be overwritten without --force")
	credsContent, err := os.ReadFile(filepath.Join(awsDir, "credentials"))
	require.NoError(t, err)
	assert.Contains(t, string(credsContent), "WRONG", "credentials must not be overwritten without --force")
}

func TestSetupAWSNonInteractiveForceOverwrites(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	awsDir := filepath.Join(tmpHome, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://example.com:9999\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = WRONG\naws_secret_access_key = WRONG\n"), 0600))

	_, _, err := runLstk(t, testContext(t), "", baseEnv, "setup", "aws", "--force")
	requireExitCode(t, 0, err)

	configContent, err := os.ReadFile(filepath.Join(awsDir, "config"))
	require.NoError(t, err)
	assert.NotContains(t, string(configContent), "example.com", "--force should overwrite the stale endpoint")
	assert.Contains(t, string(configContent), "endpoint_url")
	credsContent, err := os.ReadFile(filepath.Join(awsDir, "credentials"))
	require.NoError(t, err)
	normalizedCreds := strings.Join(strings.Fields(string(credsContent)), " ")
	assert.NotContains(t, normalizedCreds, "WRONG", "--force should overwrite stale credentials")
	assert.Contains(t, normalizedCreds, "aws_access_key_id = test")
}
