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
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	// Use a fresh HOME so a real localstack profile doesn't affect the args output.
	e := env.With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ENDPOINT:http://")
	assert.Contains(t, stdout, "ARGS:s3 ls")
	assertCommandTelemetry(t, events, "aws", 0)
}

func TestAWSCommandFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeAWS(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	e := env.With("PATH", fakeDir).
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.Contains(t, stdout, "lstk")
	assertCommandTelemetry(t, events, "aws", 1)
}

// writeSlowFakeAWS creates a fake `aws` script that sleeps for the given duration
// before printing, so the spinner has time to render in PTY-based tests.
func writeSlowFakeAWS(t *testing.T, sleepSeconds int) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}

	script := fmt.Sprintf(`#!/bin/sh
sleep %d
echo "ENDPOINT:$2"
shift 2
echo "ARGS:$@"
`, sleepSeconds)
	path := filepath.Join(dir, "aws")
	require.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return dir
}

func TestAWSCommandShowsSpinnerForSlowOperation(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	// A running emulator is required: without it, `lstk aws` exits before reaching the spinner.
	startTestContainer(t, ctx)

	fakeDir := writeSlowFakeAWS(t, 5)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)
	// /bin and /usr/bin are needed so the fake script can invoke `sleep`.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir+":/bin:/usr/bin").With(env.Home, homeDir)

	out, err := runLstkInPTY(t, ctx, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.Contains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:--profile localstack s3 ls")
}

func TestAWSCommandSuppressesSpinnerInNonInteractiveMode(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	// A running emulator is required: without it, `lstk aws` exits before reaching the spinner.
	startTestContainer(t, ctx)

	// A slow operation would normally render the spinner in a PTY; --non-interactive
	// must suppress it so captured streams carry no ANSI control codes.
	fakeDir := writeSlowFakeAWS(t, 5)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir+":/bin:/usr/bin").With(env.Home, homeDir)

	out, err := runLstkInPTY(t, ctx, e, "--non-interactive", "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.NotContains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:--profile localstack s3 ls")
}

func TestAWSCommandSuppressesSpinnerForFastOperation(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	// A running emulator is required: without it, `lstk aws` exits before reaching the spinner.
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	out, err := runLstkInPTY(t, ctx, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.NotContains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:--profile localstack s3 ls")
}
