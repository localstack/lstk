package integration_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeAWS creates a shell script that mimics `aws` by printing its args and env vars.
// Returns the directory containing the script (to prepend to PATH).
func writeFakeAWS(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}

	script := `#!/bin/sh
echo "ENDPOINT:$2"
shift 2
echo "ARGS:$@"
echo "AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "AWS_DEFAULT_REGION=$AWS_DEFAULT_REGION"
`
	path := filepath.Join(dir, "aws")
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return dir
}

// writeAWSProfile writes a minimal localstack AWS profile to dir/.aws/{config,credentials}.
func writeAWSProfile(t *testing.T, homeDir string) {
	t.Helper()
	awsDir := filepath.Join(homeDir, ".aws")
	require.NoError(t, os.MkdirAll(awsDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://localhost.localstack.cloud:4566\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
		[]byte("[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0600))
}

func TestAWSCommandInjectsEndpointAndArgs(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	// Use a fresh HOME so a real localstack profile doesn't affect the args output.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ENDPOINT:http://")
	assert.Contains(t, stdout, "ARGS:s3 ls")
}

func TestAWSCommandInjectsCredentials(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	// Use a fresh HOME so no localstack profile exists; credentials are injected via env vars.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "sts", "get-caller-identity")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=us-east-1")
}

func TestAWSCommandRespectsExistingCredentials(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	// Use a fresh HOME so no localstack profile exists; the user-provided env vars are preserved.
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "custom-key").
		With("AWS_SECRET_ACCESS_KEY", "custom-secret").
		With("AWS_DEFAULT_REGION", "eu-west-1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=custom-secret")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=eu-west-1")
}

func TestAWSCommandUsesProfileWhenAvailable(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "--profile localstack")
	// Credentials must not be injected via env when the profile is in use.
	assert.NotContains(t, stdout, "AWS_ACCESS_KEY_ID=test")
}

func TestAWSCommandFailsWhenAWSCLINotInstalled(t *testing.T) {
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	assert.Contains(t, stderr, "aws CLI not found")
}

func TestAWSCommandUsesDefaultPortWithoutConfig(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()) // isolate from any real config file

	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, ":4566")
}

func TestAWSCommandUsesPortFromConfig(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	workDir := t.TempDir()

	configContent := `
[[containers]]
type = "aws"
tag = "latest"
port = "4599"
`
	lstkDir := filepath.Join(workDir, ".lstk")
	require.NoError(t, os.MkdirAll(lstkDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"), []byte(configContent), 0644))

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir)

	stdout, stderr, err := runLstk(t, testContext(t), workDir, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, ":4599")
}

// writeFakeAWSFailing creates a shell script that mimics a failing `aws` command.
// Returns the directory containing the script (to prepend to PATH).
func writeFakeAWSFailing(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}

	script := fmt.Sprintf(`#!/bin/sh
echo "aws: error: simulated failure" >&2
exit %d
`, exitCode)
	path := filepath.Join(dir, "aws")
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return dir
}

func TestAWSCommandPropagatesExitCode(t *testing.T) {
	fakeDir := writeFakeAWSFailing(t, 42)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err, "lstk aws should fail when aws command fails")
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 42, err)
}

func TestAWSCommandHintsSetupCommandWhenProfileMissing(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err)
	assert.Contains(t, stdout, "lstk setup aws")
}

func TestAWSCommandSuppressesHintWhenProfileExists(t *testing.T) {
	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "lstk setup aws")
}
