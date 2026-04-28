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

func TestStartPromptsWhenAWSProfileMissingEverywhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)
	te := envWithDockerHostFull(t, daemon)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = te.BaseEnv.With(env.APIEndpoint, mockServer.URL)

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
	require.NoError(t, err, "lstk start should exit successfully")

	configContent, err := os.ReadFile(filepath.Join(te.Home, ".aws", "config"))
	require.NoError(t, err, "~/.aws/config should have been created")
	assert.Contains(t, string(configContent), "[profile localstack]")
	assert.Contains(t, string(configContent), "endpoint_url")

	credsContent, err := os.ReadFile(filepath.Join(te.Home, ".aws", "credentials"))
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
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)
	te := envWithDockerHostFull(t, daemon)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	awsDir := filepath.Join(te.Home, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://127.0.0.1:4566\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0600))

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = te.BaseEnv.With(env.APIEndpoint, mockServer.URL)

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
		return bytes.Contains(out.Bytes(), []byte(" ready"))
	}, 2*time.Minute, 200*time.Millisecond, "container should become ready")

	_ = cmd.Process.Kill()
	err = cmd.Wait()
	<-outputCh
	require.Error(t, err, "lstk start should exit after kill")

	assert.NotContains(t, out.String(), awsSetupPrompt,
		"profile prompt should not appear when profile is already correctly configured")
}

const awsSetupPrompt = "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?"

func TestStartNonInteractiveEmitsNoteWhenAWSProfileMissing(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)
	te := envWithDockerHostFull(t, daemon)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	stdout, _, err := runLstk(t, testContext(t), "",
		te.BaseEnv.With(env.APIEndpoint, mockServer.URL),
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
	t.Parallel()
	daemon := startEphemeralDocker(t, localstackProImage)
	te := envWithDockerHostFull(t, daemon)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	awsDir := filepath.Join(te.Home, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0600))

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = te.BaseEnv.With(env.APIEndpoint, mockServer.URL)

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

func TestConfigProfileCreatesAWSProfileWhenConfirmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	t.Parallel()
	tmpHome := t.TempDir()
	baseEnv := env.With(env.Home, tmpHome)

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "config", "profile")
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

func TestSetupAWSCreatesAWSProfileWhenConfirmed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	t.Parallel()
	tmpHome := t.TempDir()
	baseEnv := env.With(env.Home, tmpHome)

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

func TestConfigProfileDoesNotCreateAWSProfileWhenDeclined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}
	t.Parallel()
	tmpHome := t.TempDir()
	baseEnv := env.With(env.Home, tmpHome)

	ctx := testContext(t)
	cmd := exec.CommandContext(ctx, binaryPath(), "config", "profile")
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

func TestSetupAWSNonInteractiveReturnsError(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	baseEnv := env.With(env.Home, tmpHome)

	_, stderr, err := runLstk(t, testContext(t), "", baseEnv, "setup", "aws")
	require.Error(t, err)
	assert.Contains(t, stderr, "setup aws requires an interactive terminal")
}
