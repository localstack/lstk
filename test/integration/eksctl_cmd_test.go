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
echo "ENV_AWS_ENDPOINT_URL_SSM=${AWS_ENDPOINT_URL_SSM:-<unset>}"
echo "ENV_AWS_CLOUDTRAIL_ENDPOINT=${AWS_CLOUDTRAIL_ENDPOINT:-<unset>}"
echo "ENV_AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=${AWS_IGNORE_CONFIGURED_ENDPOINT_URLS:-<unset>}"
echo "ENV_AWS_REGION=$AWS_REGION"
echo "ENV_AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "ENV_AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "ENV_AWS_PROFILE=${AWS_PROFILE:-<unset>}"
echo "ENV_AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN:-<unset>}"
`, version)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "eksctl"), []byte(script), 0755))
	return dir
}

func eksctlTestEnv(t *testing.T, path string) env.Environ {
	t.Helper()
	return env.Environ(testEnvWithHome(t.TempDir(), "")).
		With(env.DisableEvents, "1").
		With(env.Path, path)
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
	e := eksctlTestEnv(t, fakeDir)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "0.150.0")
}

// --help (and -h) never require the emulator and are forwarded to eksctl.
func TestEksctlHelpNoEmulator(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"--help=true"}, {"-h"}, {"create", "cluster", "--help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeEksctl(t, "0.211.0")
			e := eksctlTestEnv(t, fakeDir)

			cmdArgs := append([]string{"eksctl"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
		})
	}
}

// an unsupported or ambiguous eksctl version fails before an AWS-contacting
// command runs.
func TestEksctlRejectsUnsupportedVersion(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)
	startTestContainer(t, ctx)

	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "too old", version: "0.180.0"},
		{name: "untrusted extra output", version: "warning: built with Go 1.25.0\n0.180.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeDir := writeFakeEksctl(t, tc.version)
			e := eksctlTestEnv(t, fakeDir)

			stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "eksctl", "get", "clusters")
			require.Error(t, err)
			assert.Contains(t, stderr+stdout, "0.181.0")
			assert.NotContains(t, stdout, "ARGS:get")
		})
	}
}

// a missing eksctl binary yields the install error.
func TestEksctlMissingBinary(t *testing.T) {
	t.Parallel()
	e := eksctlTestEnv(t, t.TempDir())

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
	e := eksctlTestEnv(t, dir).
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
	e := eksctlTestEnv(t, fakeDir)

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
	// Empty AWS defaults must behave like unset values, while ambient endpoint
	// and profile settings must not escape to the subprocess.
	e := eksctlTestEnv(t, fakeDir).
		Without(env.Key("AWS_ENDPOINT_URL")).
		With(env.AWSAccessKeyID, "").
		With(env.AWSSecretAccessKey, "").
		With(env.Key("AWS_REGION"), "").
		With(env.Key("AWS_DEFAULT_REGION"), "").
		With(env.Key("AWS_PROFILE"), "my-real-profile").
		With(env.Key("AWS_SESSION_TOKEN"), "realtoken").
		With(env.Key("AWS_ENDPOINT_URL_SSM"), "https://ssm.us-east-1.amazonaws.com").
		With(env.Key("AWS_CLOUDTRAIL_ENDPOINT"), "https://cloudtrail.us-east-1.amazonaws.com").
		With(env.Key("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS"), "true")

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
	// Higher-precedence endpoint settings cannot bypass the generic LocalStack
	// endpoint used by clients such as SSM and CloudTrail.
	assert.Contains(t, stdout, "ENV_AWS_ENDPOINT_URL_SSM=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_CLOUDTRAIL_ENDPOINT=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=false")
	// Credential defaults are applied.
	assert.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, stdout, "ENV_AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, stdout, "ENV_AWS_REGION=us-east-1")
	// Ambient AWS config is stripped.
	assert.Contains(t, stdout, "ENV_AWS_PROFILE=<unset>")
	assert.Contains(t, stdout, "ENV_AWS_SESSION_TOKEN=<unset>")

	const override = "http://eksctl-override.example.test:4567"
	overrideEnv := e.With(env.Key("AWS_ENDPOINT_URL"), override)
	overrideOut, overrideErrOut, err := runLstk(t, ctx, t.TempDir(), overrideEnv, "eksctl", "get", "clusters")
	require.NoError(t, err, "stderr: %s", overrideErrOut)
	assert.Contains(t, overrideOut, "ENV_AWS_EKS_ENDPOINT="+override)
	assert.Contains(t, overrideOut, "ENV_AWS_ENDPOINT_URL="+override)
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
	e := eksctlTestEnv(t, fakeDir)

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
	e := eksctlTestEnv(t, fakeDir)

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "eksctl", "info")
	require.Error(t, err)
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 7, err)
}
