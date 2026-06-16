package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeSAM creates a stub `sam` that answers `--version` with the given
// version string and, for any other invocation, echoes its args and the AWS
// environment it was given so tests can assert what lstk injected/stripped.
func writeFakeSAM(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake sam script not supported on Windows")
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "SAM CLI, version %s"
  exit 0
fi
echo "ARGS:$*"
echo "ENV_AWS_ENDPOINT_URL=$AWS_ENDPOINT_URL"
echo "ENV_AWS_ENDPOINT_URL_S3=${AWS_ENDPOINT_URL_S3:-<unset>}"
echo "ENV_AWS_REGION=$AWS_REGION"
echo "ENV_AWS_DEFAULT_REGION=$AWS_DEFAULT_REGION"
echo "ENV_AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "ENV_AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "ENV_AWS_PROFILE=${AWS_PROFILE:-<unset>}"
echo "ENV_AWS_DEFAULT_PROFILE=${AWS_DEFAULT_PROFILE:-<unset>}"
echo "ENV_AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-<unset>}"
`, version)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sam"), []byte(script), 0755))
	return dir
}

// writeFakeSAMExit creates a stub `sam` reporting a supported version but exiting
// with the given code for any real subcommand.
func writeFakeSAMExit(t *testing.T, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake sam script not supported on Windows")
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then echo "SAM CLI, version 1.95.0"; exit 0; fi
echo "sam: simulated failure" >&2
exit %d
`, code)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sam"), []byte(script), 0755))
	return dir
}

// forwards args. `build` is offline, so no emulator/Docker is required.
func TestSAMForwardsArgs(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ARGS:build")
}

// propagates the sam exit code.
func TestSAMPropagatesExitCode(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAMExit(t, 7)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 7, err)
}

// the subprocess gets the LocalStack-pointing env (region in AWS_DEFAULT_REGION,
// custom --account in AWS_ACCESS_KEY_ID, no S3 endpoint), and ambient AWS config
// that could redirect at real AWS is stripped.
func TestSAMInjectsCleanAWSEnv(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With(env.Key("AWS_PROFILE"), "my-real-profile").
		With(env.Key("AWS_DEFAULT_PROFILE"), "other").
		With(env.Key("AWS_SESSION_TOKEN"), "realtoken")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--region", "eu-west-1", "--account", "111111111111", "build")
	require.NoError(t, err, "stderr: %s", stderr)

	assert.Contains(t, stdout, "ENV_AWS_ENDPOINT_URL=http")
	assert.Contains(t, stdout, ":4566")
	// SAM reads AWS_DEFAULT_REGION; lstk sets both region vars.
	assert.Contains(t, stdout, "ENV_AWS_REGION=eu-west-1")
	assert.Contains(t, stdout, "ENV_AWS_DEFAULT_REGION=eu-west-1")
	// A custom --account flows through to AWS_ACCESS_KEY_ID (Terraform model).
	assert.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=111111111111")
	assert.Contains(t, stdout, "ENV_AWS_SECRET_ACCESS_KEY=test")
	// lstk never sets an S3-specific endpoint for SAM.
	assert.Contains(t, stdout, "ENV_AWS_ENDPOINT_URL_S3=<unset>")
	// Ambient AWS config is stripped.
	assert.Contains(t, stdout, "ENV_AWS_PROFILE=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_DEFAULT_PROFILE=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_SESSION_TOKEN=<unset>")
}

// offline subcommands run without a running emulator, even with a leading
// --region present (which is stripped from the forwarded args).
func TestSAMOfflineCommandsNoEmulator(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"init", "build", "validate", "docs", "pipeline"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeSAM(t, "1.95.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
				"sam", "--region", "us-west-2", sub)
			require.NoError(t, err, "stderr: %s", stderr)

			assert.Contains(t, stdout, "ARGS:"+sub)
			assert.NotContains(t, stdout, "--region")
		})
	}
}

// a too-old sam fails before the command runs.
func TestSAMVersionTooOld(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.94.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "1.95.0")
	// sam was never run for real.
	assert.NotContains(t, stdout, "ARGS:build")
}

// a missing sam binary yields the install error.
func TestSAMMissingBinary(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "not found in PATH")
}

// --account is supported for sam (unlike cdk): a valid 12-digit value is
// accepted and reaches the subprocess as AWS_ACCESS_KEY_ID.
func TestSAMAccountSupported(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--account", "123456789012", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=123456789012")
}

// an invalid --account value is rejected at the command boundary before sam runs.
func TestSAMInvalidAccountRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--account", "12345", "build")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "12-digit")
	assert.NotContains(t, stdout, "ARGS:build")
}

// flags after the subcommand are forwarded to sam unchanged.
func TestSAMFlagsAfterActionAreForwarded(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "build", "--region", "us-west-2")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ARGS:build --region us-west-2")
}

// a flag before the subcommand is rejected with a clear message.
func TestSAMFlagBeforeSubcommandRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--account", "111111111111", "sam", "build")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "must appear after the sam subcommand")
}

// LSTK_SAM_CMD selects the binary to invoke.
func TestSAMHonorsLstkSamCmd(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake sam script not supported on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mysam"),
		[]byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"SAM CLI, version 1.95.0\"; exit 0; fi\necho \"MYSAM:$*\"\n"), 0755))
	e := env.With(env.DisableEvents, "1").With("PATH", dir).With(env.Home, t.TempDir()).
		With(env.Key("LSTK_SAM_CMD"), "mysam")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "MYSAM:build")
}

// an AWS-contacting command with no running emulator fails with "not running"
// and does not invoke sam.
func TestSAMFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "deploy")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.NotContains(t, stdout, "ARGS:deploy")
}

// an AWS-contacting command fails with an AWS-specific error naming the running
// non-AWS emulator, and does not invoke sam.
func TestSAMRequiresAWSEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)
	startTestSnowflakeContainer(t, ctx)

	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "sam", "deploy")
	require.Error(t, err)
	assert.Contains(t, stdout, "requires the")
	assert.Contains(t, stdout, "Snowflake")
	assert.NotContains(t, stdout, "ARGS:deploy")
}
