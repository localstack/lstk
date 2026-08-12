package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
)

// writeFakeAWS creates a shell script that mimics `aws` by printing its args and env vars.
// Returns the directory containing the script (to prepend to PATH).
//
// Credential variables are printed with ${VAR-<unset>} rather than plain $VAR so
// tests can tell "removed from the environment" apart from "present but empty" —
// the distinction the profile path turns on. lstk no longer passes --profile, so
// when an endpoint is injected it is always the first two args.
//
// The endpoint is matched rather than assumed: the help path runs the AWS CLI
// with no --endpoint-url at all, and an unconditional `shift 2` there aborts the
// script under dash ("can't shift that many"), which is /bin/sh on Ubuntu though
// not on macOS.
func writeFakeAWS(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if runtime.GOOS == "windows" {
		t.Skip("fake aws script not supported on Windows")
	}

	script := `#!/bin/sh
if [ "$1" = "--endpoint-url" ]; then
  echo "ENDPOINT:$2"
  shift 2
else
  echo "ENDPOINT:<none>"
fi
echo "ARGS:$@"
echo "AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID-<unset>}"
echo "AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY-<unset>}"
echo "AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN-<unset>}"
echo "AWS_DEFAULT_REGION=${AWS_DEFAULT_REGION-<unset>}"
echo "AWS_PROFILE=${AWS_PROFILE-<unset>}"
`
	path := filepath.Join(dir, "aws")
	must.NoError(t, os.WriteFile(path, []byte(script), 0755))
	return dir
}

// writeAWSProfile writes a minimal localstack AWS profile to dir/.aws/{config,credentials}.
func writeAWSProfile(t *testing.T, homeDir string) {
	t.Helper()
	awsDir := filepath.Join(homeDir, ".aws")
	must.NoError(t, os.MkdirAll(awsDir, 0700))
	must.NoError(t, os.WriteFile(filepath.Join(awsDir, "config"),
		[]byte("[profile localstack]\nregion = us-east-1\noutput = json\nendpoint_url = http://localhost.localstack.cloud:4566\n"), 0600))
	must.NoError(t, os.WriteFile(filepath.Join(awsDir, "credentials"),
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
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "ENDPOINT:http://")
	must.Contains(t, stdout, "ARGS:s3 ls")
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
	must.NoError(t, os.WriteFile(configPath, []byte("# lstk test config\n"), 0600))

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "ARGS:s3 ls")
	must.NotContains(t, stdout, "--config")
	must.NotContains(t, stdout, "--non-interactive")
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
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
	must.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	must.Contains(t, stdout, "AWS_DEFAULT_REGION=us-east-1")
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
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=custom-key")
	must.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=custom-secret")
	must.Contains(t, stdout, "AWS_DEFAULT_REGION=eu-west-1")
}

// The profile is selected through AWS_PROFILE, never a --profile argument: an
// explicitly named profile removes botocore's environment credential provider,
// which is what made account selection impossible.
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
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_PROFILE=localstack")
	must.NotContains(t, stdout, "--profile")
	// The profile is the sole credentials source: lstk removes the variables
	// rather than seeding over them (7.10).
	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=<unset>")
	must.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=<unset>")
	must.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
	// No default region is seeded over the profile's own (7.12).
	must.Contains(t, stdout, "AWS_DEFAULT_REGION=<unset>")
}

// 7.1 — the path that already worked: with no profile configured, a 12-digit
// AWS_ACCESS_KEY_ID reaches the AWS CLI and selects that LocalStack account.
func TestAWSCommandAccountFromEnvWithoutProfile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
}

// 7.2 — the silent failure this change fixes: the same command stopped working
// once `lstk setup aws` had run, because --profile localstack made botocore
// ignore the environment credentials entirely.
func TestAWSCommandAccountFromEnvWithProfile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, homeDir).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
	must.NotContains(t, stdout, "--profile")
	// The profile is still selected, so its other settings keep applying.
	must.Contains(t, stdout, "AWS_PROFILE=localstack")
}

// 7.3 — the flag, with the profile present so the bypass path is exercised.
func TestAWSCommandAccountFlag(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--account", "111111111111", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
	// The flag is lstk's: it must not reach the AWS CLI.
	must.Contains(t, stdout, "ARGS:s3 ls")
	// A secret is always supplied alongside the key — a lone access key id
	// makes the AWS CLI fail with "Partial credentials found in env" (7.11).
	must.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	// No default region is seeded over the profile's own (7.12).
	must.Contains(t, stdout, "AWS_DEFAULT_REGION=<unset>")
}

// 7.4
func TestAWSCommandAccountFlagBeatsEnv(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--account=222222222222", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=222222222222")
	must.NotContains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
}

// 7.9 — a real key in the environment is not an account selection: the profile
// keeps supplying credentials, and the live key value never reaches the child.
func TestAWSCommandRealAccessKeyDoesNotDisplaceProfile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	homeDir := t.TempDir()
	writeAWSProfile(t, homeDir)

	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, homeDir).
		With("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE").
		With("AWS_SECRET_ACCESS_KEY", "realsecret").
		With("AWS_SESSION_TOKEN", "realtoken")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_PROFILE=localstack")
	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=<unset>")
	must.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=<unset>")
	must.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
	must.NotContains(t, stdout, "AKIAIOSFODNN7EXAMPLE")
	must.NotContains(t, stdout, "realsecret")
}

// 7.9 (no-profile half) — with nothing else to supply credentials the key is
// passed through, but deactivated so the live value never reaches LocalStack.
func TestAWSCommandDeactivatesRealAccessKeyWithoutProfile(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").
		With("PATH", fakeDir).
		With(env.Home, t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE").
		With("AWS_SESSION_TOKEN", "realtoken")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=LKIAIOSFODNN7EXAMPLE")
	must.NotContains(t, stdout, "AKIAIOSFODNN7EXAMPLE")
	must.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
}

// 7.7 — a --account after the AWS service belongs to the AWS CLI
// (e.g. `organizations describe-account --account-id`).
func TestAWSCommandForwardsNonLeadingAccountFlag(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls", "--account", "111111111111")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "ARGS:s3 ls --account 111111111111")
	must.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
}

// 7.8 — --region is the AWS CLI's own flag and is never consumed by lstk, even
// in the leading position where the IaC proxies would claim it.
func TestAWSCommandForwardsRegionFlag(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--region", "us-west-2", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, "ARGS:--region us-west-2 s3 ls")
}

// 7.5 — rejected at the command boundary, before the AWS CLI is invoked. No
// emulator needed: the failure precedes endpoint resolution.
func TestAWSCommandRejectsInvalidAccount(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account", "12345", "s3", "ls")
	must.Error(t, err)
	must.Contains(t, stdout, "12-digit AWS account id")
	// The AWS CLI must not have run.
	must.NotContains(t, stdout, "ARGS:")
}

// 7.5 (missing value)
func TestAWSCommandRejectsAccountWithoutValue(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account")
	must.Error(t, err)
	must.Contains(t, stdout, "--account requires a value")
	must.NotContains(t, stdout, "ARGS:")
}

// 7.6 — a flag before the `aws` token would be eaten during Cobra's command
// resolution, so it is rejected with a placement error rather than dropped.
func TestAWSCommandRejectsPreSubcommandAccount(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "--account", "111111111111", "aws", "s3", "ls")
	must.Error(t, err)
	must.Contains(t, stdout, "must appear after the aws subcommand")
	must.NotContains(t, stdout, "ARGS:")
}

// The leading flag is consumed before the help short-circuit, so help still
// works without an emulator and without forwarding a flag the AWS CLI rejects.
func TestAWSCommandAccountWithHelp(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account", "111111111111", "help")
	must.NoError(t, err, "lstk aws help failed: %s", stderr)

	must.NotContains(t, stdout, "--account")
}

func TestAWSCommandFailsWhenAWSCLINotInstalled(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	must.Error(t, err)
	must.Contains(t, stdout, "aws CLI not found in PATH")
	must.Contains(t, stdout, "Install AWS CLI:")
	must.Contains(t, stdout, "https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html")
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
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, ":4566")
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
	must.NoError(t, os.MkdirAll(lstkDir, 0755))
	must.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"), []byte(configContent), 0644))

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir)

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws failed: %s", stderr)

	must.Contains(t, stdout, ":4599")
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
	must.NoError(t, os.WriteFile(path, []byte(script), 0755))
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
	must.Error(t, err, "lstk aws should fail when aws command fails")
	must.Contains(t, stderr, "simulated failure")
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
	must.NoError(t, os.WriteFile(filepath.Join(dir, "aws"), []byte(script), 0755))

	for _, args := range [][]string{{"--help"}, {"-h"}, {"s3", "--help"}, {"help"}, {"s3", "help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := env.With(env.DisableEvents, "1").
				With("PATH", dir).
				With(env.Home, t.TempDir()).
				With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

			cmdArgs := append([]string{"aws"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			must.NoError(t, err, "stderr: %s", stderr)

			must.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
			must.NotContains(t, stdout, "--endpoint-url")
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
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	must.Error(t, err)
	must.Contains(t, stdout, "Docker is not available")
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
	must.Error(t, err)
	must.Contains(t, stdout, "is not running")
	must.Contains(t, stdout, "Start LocalStack:")
	must.Contains(t, stdout, "lstk")
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
	must.NoError(t, err)
	must.Contains(t, stdout, "lstk setup aws")
}

func TestAWSCommandWorksWithExternalContainer(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	ctx := testContext(t)

	const fakeImage = "localstack/localstack-pro:test-fake"
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	must.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-main", "4566")

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	must.NoError(t, err, "lstk aws should work with externally-named container: %s", stderr)
	must.Contains(t, stdout, "ENDPOINT:http://")
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
	must.NoError(t, os.WriteFile(path, []byte(script), 0755))
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
	must.NoError(t, err, "lstk aws failed: %s", out)

	must.Contains(t, out, "Loading service")
	must.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	must.NotContains(t, out, "--profile")
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
	must.NoError(t, err, "lstk aws failed: %s", out)

	must.NotContains(t, out, "Loading service")
	must.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	must.NotContains(t, out, "--profile")
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
	must.NoError(t, err, "lstk aws failed: %s", out)

	must.NotContains(t, out, "Loading service")
	must.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	must.NotContains(t, out, "--profile")
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
	must.NoError(t, err)
	must.NotContains(t, stdout, "lstk setup aws")
}
