package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/snap"
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
	scheduleVolumeCleanup(t, tmpHome)
	writeConfigFile(t, filepath.Join(tmpHome, ".config", "lstk", "config.toml"))
	e := env.With(env.AuthToken, env.Get(env.AuthToken)).WithHome(tmpHome)
	return e, tmpHome
}

func TestStartPromptsWhenAWSProfileMissingEverywhere(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	t.Cleanup(cleanup)

	baseEnv, tmpHome := awsConfigEnv(t)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	ctx := testContext(t)
	p := startLstkInPTY(t, ctx, baseEnv.With(env.APIEndpoint, mockServer.URL), "start")

	// Wait for the prompt emitted after the container becomes ready.
	p.waitForOutputTimeout(awsSetupPrompt, 2*time.Minute, "AWS profile prompt should appear")
	p.write("y")

	_, err := p.wait()
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
	p := startLstkInPTY(t, ctx, baseEnv.With(env.APIEndpoint, mockServer.URL), "start")

	// Wait until the container is ready — that's the point at which post-start setup
	// runs, so if the prompt were going to appear it would already be in the output.
	p.waitForOutputTimeout("LocalStack is running", 2*time.Minute, "container should become ready")

	// Teardown only: lstk may already have exited on its own, so don't assert on Wait's error.
	_ = p.cmd.Process.Kill()
	out, _ := p.wait()

	assert.NotContains(t, out, awsSetupPrompt,
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
	p := startLstkInPTY(t, ctx, baseEnv.With(env.APIEndpoint, mockServer.URL), "start")

	p.waitForOutputTimeout("LocalStack AWS profile is incomplete. Run 'lstk setup aws'.", 2*time.Minute, "AWS profile note should appear")

	out, err := p.wait()
	require.NoError(t, err, "lstk start should exit successfully")

	assert.NotContains(t, out, "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?",
		"profile prompt should not appear for a partial setup")
}

func TestSetupAWSCreatesAWSProfileWhenConfirmed(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	ctx := testContext(t)
	p := startLstkInPTY(t, ctx, baseEnv, "setup", "aws")

	// Wait for the AWS profile prompt, then press Y to confirm.
	p.waitForOutput(awsSetupPrompt, "AWS profile prompt should appear")
	p.write("y")

	out, err := p.wait()
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

	assert.Contains(t, out, "Created LocalStack profile in ~/.aws")
	assert.NotContains(t, out, "Skipped adding LocalStack AWS profile.")
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
		t.Skip("read-only directory permissions are not enforced on Windows, so the profile write would not fail")
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
	p := startLstkInPTY(t, ctx, baseEnv, "setup", "aws")

	p.waitForOutput(awsSetupPrompt, "AWS profile prompt should appear")
	p.write("y")

	out, err := p.wait()
	requireExitCode(t, 1, err)

	assert.Contains(t, out, "Could not set up the LocalStack AWS profile")
	assert.NotContains(t, out, "Created LocalStack profile")
}

func TestSetupAWSDoesNotCreateAWSProfileWhenDeclined(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	ctx := testContext(t)
	p := startLstkInPTY(t, ctx, baseEnv, "setup", "aws")

	p.waitForOutput(awsSetupPrompt, "AWS profile prompt should appear")
	p.write("n")

	out, err := p.wait()
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(tmpHome, ".aws", "config"))
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(tmpHome, ".aws", "credentials"))
	assert.ErrorIs(t, err, os.ErrNotExist)

	assert.Contains(t, out, "Skipped adding LocalStack AWS profile.")
	assert.NotContains(t, out, "Created LocalStack profile in ~/.aws/config")
}

func TestSetupAWSNonInteractiveCreatesProfile(t *testing.T) {
	t.Parallel()
	baseEnv, tmpHome := awsConfigEnv(t)

	stdout, _, err := runLstk(t, testContext(t), "",
		baseEnv,
		"setup", "aws",
	)
	requireExitCode(t, 0, err)
	snap.Match(t, sanitizeOutput(stdout))

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
	snap.Match(t, sanitizeOutput(stdout))
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
	snap.Match(t, sanitizeOutput(stdout))

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
