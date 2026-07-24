package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/moby/moby/client"
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

func TestAWSCommandStripsGlobalFlagsFromPassthrough(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	// --config must resolve to this file, not be forwarded to the aws binary.
	configPath := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("# lstk test config\n"), 0600))

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:--profile localstack s3 ls")
	assert.NotContains(t, stdout, "--config")
	assert.NotContains(t, stdout, "--non-interactive")
}

func TestAWSCommandInjectsCredentials(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	// Use a fresh HOME so no localstack profile exists; credentials are injected via env vars.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "sts", "get-caller-identity")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=us-east-1")
}

func TestAWSCommandRespectsExistingCredentials(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	// Use a fresh HOME so no localstack profile exists; the user-provided env vars are preserved.
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "custom-key").
		With("AWS_SECRET_ACCESS_KEY", "custom-secret").
		With("AWS_DEFAULT_REGION", "eu-west-1")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=custom-secret")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=eu-west-1")
}

func TestAWSCommandUsesProfileWhenAvailable(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "--profile localstack")
	// Credentials must not be injected via env when the profile is in use.
	assert.NotContains(t, stdout, "AWS_ACCESS_KEY_ID=test")
}

func TestAWSCommandFailsWhenAWSCLINotInstalled(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	assert.Contains(t, stdout, "aws CLI not found in PATH")
	assert.Contains(t, stdout, "Install AWS CLI:")
	assert.Contains(t, stdout, "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html")
}

func TestAWSCommandUsesDefaultPortWithoutConfig(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()) // isolate from any real config file

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, ":4566")
}

func TestAWSCommandUsesPortFromConfig(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

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

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "aws", "s3", "ls")
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
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWSFailing(t, 42)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir)

	_, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err, "lstk aws should fail when aws command fails")
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 42, err)
}

// DEVX-1002 — --help/-h, and the bare "help" pseudo-subcommand, never contact
// LocalStack, so they run without Docker/an emulator: DOCKER_HOST points at an
// unreachable address (mirroring TestAWSCommandFailsWhenDockerNotRunning) yet
// the command still succeeds and forwards the help request untouched, with no
// --endpoint-url injected.
func TestAWSCommandHelpSkipsDockerAndEmulator(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Docker error tested separately via windowsDockerErrorEnv")
	}

	dir := t.TempDir()
	script := "#!/bin/sh\necho \"ARGS:$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0755))

	for _, args := range [][]string{{"--help"}, {"-h"}, {"s3", "--help"}, {"help"}, {"s3", "help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := env.With(env.DisableEvents, "1").
				With("PATH", dir).
				With(env.Home, t.TempDir()).
				With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

			cmdArgs := append([]string{"aws"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)

			assert.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
			assert.NotContains(t, stdout, "--endpoint-url")
		})
	}
}

func TestAWSCommandFailsWhenDockerNotRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows Docker error tested separately via windowsDockerErrorEnv")
	}

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1").
		With(env.LocalStackHost, deadLocalStackHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	assert.Contains(t, stdout, "Docker is not available")
}

func TestAWSCommandFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeAWS(t)
	analyticsSrv, events := mockAnalyticsServer(t)
	e := env.With("PATH", fakeDir).
		With(env.AnalyticsEndpoint, analyticsSrv.URL).
		With(env.LocalStackHost, deadLocalStackHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.Contains(t, stdout, "lstk")
	assertCommandTelemetry(t, events, "aws", 1)
}

func TestAWSCommandHintsSetupCommandWhenProfileMissing(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err)
	assert.Contains(t, stdout, "lstk setup aws")
}

func TestAWSCommandWorksWithExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage}); require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-main", "4566")

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws should work with externally-named container: %s", stderr)
	assert.Contains(t, stdout, "ENDPOINT:http://")
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

func TestAWSCommandSuppressesHintWhenProfileExists(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "lstk setup aws")
}
