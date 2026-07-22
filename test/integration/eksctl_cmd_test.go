package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeEksctl creates a stub `eksctl` that answers `version` with the given
// version string and, for any other invocation, echoes its args and the AWS
// environment it was given so tests can assert what lstk injected/stripped.
func writeFakeEksctl(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake eksctl script not supported on Windows")
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then
  echo "%s"
  exit 0
fi
echo "ARGS:$*"
echo "ENV_AWS_EKS_ENDPOINT=${AWS_EKS_ENDPOINT:-<unset>}"
echo "ENV_AWS_CLOUDFORMATION_ENDPOINT=${AWS_CLOUDFORMATION_ENDPOINT:-<unset>}"
echo "ENV_AWS_STS_ENDPOINT=${AWS_STS_ENDPOINT:-<unset>}"
echo "ENV_AWS_IAM_ENDPOINT=${AWS_IAM_ENDPOINT:-<unset>}"
echo "ENV_AWS_ENDPOINT_URL=${AWS_ENDPOINT_URL:-<unset>}"
echo "ENV_AWS_REGION=$AWS_REGION"
echo "ENV_AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "ENV_AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "ENV_AWS_PROFILE=${AWS_PROFILE:-<unset>}"
echo "ENV_AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-<unset>}"
`, version)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "eksctl"), []byte(script), 0755))
	return dir
}

// writeFakeEksctlExit creates a stub `eksctl` reporting a supported version but
// exiting with the given code for any real subcommand.
func writeFakeEksctlExit(t *testing.T, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake eksctl script not supported on Windows")
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "version" ]; then echo "0.211.0"; exit 0; fi
echo "eksctl: simulated failure" >&2
exit %d
`, code)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "eksctl"), []byte(script), 0755))
	return dir
}

// offline subcommands (version) run without a running emulator or Docker, and
// without the minimum-version gate — a too-old eksctl can still report itself.
func TestEksctlVersionNoEmulator(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeEksctl(t, "0.150.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "0.150.0")
}

// --help (and -h) never require the emulator and are forwarded to eksctl.
func TestEksctlHelpNoEmulator(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"create", "cluster", "--help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeEksctl(t, "0.211.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

			cmdArgs := append([]string{"eksctl"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
		})
	}
}

// a too-old eksctl fails before an AWS-contacting command runs.
func TestEksctlVersionTooOld(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeEksctl(t, "0.180.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "eksctl", "get", "clusters")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "0.181.0")
	// eksctl was never run for real.
	assert.NotContains(t, stdout, "ARGS:get")
}

// a missing eksctl binary yields the install error.
func TestEksctlMissingBinary(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "version")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "not found in PATH")
}

// LSTK_EKSCTL_CMD selects the binary to invoke.
func TestEksctlHonorsLstkEksctlCmd(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake eksctl script not supported on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myeksctl"),
		[]byte("#!/bin/sh\nif [ \"$1\" = \"version\" ]; then echo \"0.211.0\"; exit 0; fi\necho \"MYEKSCTL:$*\"\n"), 0755))
	e := env.With(env.DisableEvents, "1").With("PATH", dir).With(env.Home, t.TempDir()).
		With(env.Key("LSTK_EKSCTL_CMD"), "myeksctl")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "info")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "MYEKSCTL:info")
}

// an AWS-contacting command with no running emulator fails with "not running"
// and does not invoke eksctl.
func TestEksctlFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeEksctl(t, "0.211.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "get", "clusters")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.NotContains(t, stdout, "ARGS:get")
}

// an AWS-contacting command against a running AWS emulator forwards args and
// injects the LocalStack service endpoints, credential defaults, and strips
// ambient AWS config that could redirect at real AWS.
func TestEksctlInjectsCleanAWSEnv(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeEksctl(t, "0.211.0")
	// Strip ambient values the set-if-absent assertions below depend on, so a
	// developer shell exporting real AWS config cannot fail the test.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		Without(env.AWSAccessKeyID, env.AWSSecretAccessKey,
			env.Key("AWS_REGION"), env.Key("AWS_DEFAULT_REGION"), env.Key("AWS_ENDPOINT_URL")).
		With(env.Key("AWS_PROFILE"), "my-real-profile").
		With(env.Key("AWS_SESSION_TOKEN"), "realtoken")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "eksctl", "get", "clusters")
	require.NoError(t, err, "stderr: %s", stderr)

	assert.Contains(t, stdout, "ARGS:get clusters")
	// All service endpoints point at LocalStack.
	assert.Contains(t, stdout, "ENV_AWS_EKS_ENDPOINT=http")
	assert.Contains(t, stdout, "ENV_AWS_CLOUDFORMATION_ENDPOINT=http")
	assert.Contains(t, stdout, "ENV_AWS_STS_ENDPOINT=http")
	assert.Contains(t, stdout, "ENV_AWS_IAM_ENDPOINT=http")
	assert.Contains(t, stdout, "ENV_AWS_ENDPOINT_URL=http")
	assert.Contains(t, stdout, ":4566")
	// Credential defaults are applied.
	assert.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, stdout, "ENV_AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, stdout, "ENV_AWS_REGION=us-east-1")
	// Ambient AWS config is stripped.
	assert.Contains(t, stdout, "ENV_AWS_PROFILE=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_SESSION_TOKEN=<unset>")
}

// an AWS-contacting command fails with an AWS-specific error naming the running
// non-AWS emulator, and does not invoke eksctl.
func TestEksctlRequiresAWSEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)
	startTestSnowflakeContainer(t, ctx)

	fakeDir := writeFakeEksctl(t, "0.211.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "eksctl", "get", "clusters")
	require.Error(t, err)
	assert.Contains(t, stdout, "requires the")
	assert.Contains(t, stdout, "Snowflake")
	assert.NotContains(t, stdout, "ARGS:get")
}

// propagates the eksctl exit code. The offline `info` subcommand exercises the
// same eksctl.Run → proc.Run path without needing Docker or an emulator.
func TestEksctlPropagatesExitCode(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeEksctlExit(t, 7)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "info")
	require.Error(t, err)
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 7, err)
}
