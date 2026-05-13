package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeTerraform creates a shell script that mimics `terraform` by echoing
// its args plus the AWS_* env vars lstk injects (default endpoint plus any
// AWS_ENDPOINT_URL_<SERVICE> overrides that flow through). Returns the
// directory containing the script (to prepend to PATH).
func writeFakeTerraform(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()

	script := `#!/bin/sh
echo "ARGS:$@"
echo "AWS_ACCESS_KEY_ID=$AWS_ACCESS_KEY_ID"
echo "AWS_SECRET_ACCESS_KEY=$AWS_SECRET_ACCESS_KEY"
echo "AWS_DEFAULT_REGION=$AWS_DEFAULT_REGION"
echo "AWS_ENDPOINT_URL=$AWS_ENDPOINT_URL"
echo "AWS_ENDPOINT_URL_S3=$AWS_ENDPOINT_URL_S3"
echo "AWS_ENDPOINT_URL_DYNAMODB=$AWS_ENDPOINT_URL_DYNAMODB"
echo "AWS_ENDPOINT_URL_LAMBDA=$AWS_ENDPOINT_URL_LAMBDA"
if [ -f localstack_providers_override.tf ]; then
  echo "OVERRIDE_PRESENT:yes"
else
  echo "OVERRIDE_PRESENT:no"
fi
`
	path := filepath.Join(dir, "terraform")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return dir
}

func TestTerraformCommandInjectsEndpointEnv(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	workDir := t.TempDir()
	analyticsSrv, events := mockAnalyticsServer(t)
	e := env.With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "init")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:init")
	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=us-east-1")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL=http://")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_S3=http://s3.")

	// No file should ever be written into the working tree.
	assert.Contains(t, stdout, "OVERRIDE_PRESENT:no")
	_, err = os.Stat(filepath.Join(workDir, "localstack_providers_override.tf"))
	assert.True(t, os.IsNotExist(err), "lstk must not write override files")

	assertCommandTelemetry(t, events, "terraform", 0)
}

func TestTerraformCommandTfAlias(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "tf", "plan")
	require.NoError(t, err, "lstk tf failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:plan")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL=http://")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_S3=http://s3.")
}

func TestTerraformCommandFailsWhenTerraformNotInstalled(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir())

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "init")
	require.Error(t, err)
	assert.Contains(t, stderr, "terraform CLI not found")
}

func TestTerraformCommandFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "init")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
}

func TestTerraformCommandPropagatesExitCode(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
echo "terraform: simulated failure" >&2
exit 17
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0o755))

	e := env.With(env.DisableEvents, "1").
		With("PATH", dir+":/bin:/usr/bin").
		With(env.Home, t.TempDir())

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")
	require.Error(t, err)
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 17, err)
}

func TestTerraformCommandPreservesUserAWSEnvVars(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "custom-key").
		With("AWS_DEFAULT_REGION", "eu-west-1")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=eu-west-1")
}

func TestTerraformCommandForwardsCustomerEndpointEnvVars(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Home, t.TempDir()).
		// Customer-supplied AWS_ENDPOINT_URL_S3 must win over lstk's default,
		// and a service lstk doesn't know about (LAMBDA) must flow through.
		With("AWS_ENDPOINT_URL_S3", "http://customer-s3:9000").
		With("AWS_ENDPOINT_URL_LAMBDA", "http://customer-lambda:9000")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")
	require.NoError(t, err, "lstk terraform failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_S3=http://customer-s3:9000")
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_LAMBDA=http://customer-lambda:9000")
	// lstk does NOT invent a default for DYNAMODB — the var should be empty.
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL_DYNAMODB=\n")
	// And the lstk default for the bare AWS_ENDPOINT_URL is still applied.
	assert.Contains(t, stdout, "AWS_ENDPOINT_URL=http://")
}

func TestTerraformCommandFailsWhenDockerNotRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Docker error tested separately via windowsDockerErrorEnv")
	}

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir+":/bin:/usr/bin").
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1").
		With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "init")
	require.Error(t, err)
	assert.Contains(t, stdout, "Docker is not available")
}
