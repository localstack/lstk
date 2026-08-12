package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeAWS creates a fake `aws` that prints its args and env vars.
// Returns the directory containing it (to prepend to PATH).
//
// Credential variables are printed with {env:VAR-<unset>} (the sh ${VAR-x}
// form) rather than plain {env:VAR} so tests can tell "removed from the
// environment" apart from "present but empty" — the distinction the profile
// path turns on. lstk no longer passes --profile, so when an endpoint is
// injected it is always the first two args.
//
// The endpoint is matched rather than assumed: the help path runs the AWS CLI
// with no --endpoint-url at all, so the shift only happens on the matching
// case.
func writeFakeAWS(t *testing.T) string {
	t.Helper()
	tail := []string{
		"ARGS:{args}",
		"AWS_ACCESS_KEY_ID={env:AWS_ACCESS_KEY_ID-<unset>}",
		"AWS_SECRET_ACCESS_KEY={env:AWS_SECRET_ACCESS_KEY-<unset>}",
		"AWS_SESSION_TOKEN={env:AWS_SESSION_TOKEN-<unset>}",
		"AWS_DEFAULT_REGION={env:AWS_DEFAULT_REGION-<unset>}",
		"AWS_PROFILE={env:AWS_PROFILE-<unset>}",
	}
	return writeFakeTool(t, "aws", fakeToolConfig{
		Cases: []fakeToolCase{{
			Args:   []string{"--endpoint-url"},
			Shift:  2,
			Stdout: append([]string{"ENDPOINT:{arg2}"}, tail...),
		}},
		Stdout: append([]string{"ENDPOINT:<none>"}, tail...),
	})
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
	e := env.With("PATH", fakeDir).WithHome(t.TempDir()).
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

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "--config", configPath, "--non-interactive", "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:s3 ls")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

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
		WithHome(t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "custom-key").
		With("AWS_SECRET_ACCESS_KEY", "custom-secret").
		With("AWS_DEFAULT_REGION", "eu-west-1")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=custom-secret")
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=eu-west-1")
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

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_PROFILE=localstack")
	assert.NotContains(t, stdout, "--profile")
	// The profile is the sole credentials source: lstk removes the variables
	// rather than seeding over them (7.10).
	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=<unset>")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=<unset>")
	assert.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
	// No default region is seeded over the profile's own (7.12).
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=<unset>")
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
		WithHome(t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
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
		WithHome(homeDir).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
	assert.NotContains(t, stdout, "--profile")
	// The profile is still selected, so its other settings keep applying.
	assert.Contains(t, stdout, "AWS_PROFILE=localstack")
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

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--account", "111111111111", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
	// The flag is lstk's: it must not reach the AWS CLI.
	assert.Contains(t, stdout, "ARGS:s3 ls")
	// A secret is always supplied alongside the key — a lone access key id
	// makes the AWS CLI fail with "Partial credentials found in env" (7.11).
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=test")
	// No default region is seeded over the profile's own (7.12).
	assert.Contains(t, stdout, "AWS_DEFAULT_REGION=<unset>")
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
		WithHome(t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "111111111111")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--account=222222222222", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=222222222222")
	assert.NotContains(t, stdout, "AWS_ACCESS_KEY_ID=111111111111")
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
		WithHome(homeDir).
		With("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE").
		With("AWS_SECRET_ACCESS_KEY", "realsecret").
		With("AWS_SESSION_TOKEN", "realtoken")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_PROFILE=localstack")
	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=<unset>")
	assert.Contains(t, stdout, "AWS_SECRET_ACCESS_KEY=<unset>")
	assert.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
	assert.NotContains(t, stdout, "AKIAIOSFODNN7EXAMPLE")
	assert.NotContains(t, stdout, "realsecret")
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
		WithHome(t.TempDir()).
		With("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE").
		With("AWS_SESSION_TOKEN", "realtoken")

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=LKIAIOSFODNN7EXAMPLE")
	assert.NotContains(t, stdout, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, stdout, "AWS_SESSION_TOKEN=<unset>")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls", "--account", "111111111111")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:s3 ls --account 111111111111")
	assert.Contains(t, stdout, "AWS_ACCESS_KEY_ID=test")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "--region", "us-west-2", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", stderr)

	assert.Contains(t, stdout, "ARGS:--region us-west-2 s3 ls")
}

// 7.5 — rejected at the command boundary, before the AWS CLI is invoked. No
// emulator needed: the failure precedes endpoint resolution.
func TestAWSCommandRejectsInvalidAccount(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account", "12345", "s3", "ls")
	require.Error(t, err)
	// The AWS CLI must not have run, so the snapshot carries no ARGS: line.
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.5 (missing value)
func TestAWSCommandRejectsAccountWithoutValue(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.6 — a flag before the `aws` token would be eaten during Cobra's command
// resolution, so it is rejected with a placement error rather than dropped.
func TestAWSCommandRejectsPreSubcommandAccount(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "--account", "111111111111", "aws", "s3", "ls")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
}

// The leading flag is consumed before the help short-circuit, so help still
// works without an emulator and without forwarding a flag the AWS CLI rejects.
func TestAWSCommandAccountWithHelp(t *testing.T) {
	t.Parallel()

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "--account", "111111111111", "help")
	require.NoError(t, err, "lstk aws help failed: %s", stderr)

	assert.NotContains(t, stdout, "--account")
}

func TestAWSCommandFailsWhenAWSCLINotInstalled(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "aws", "s3", "ls")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stdout))
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
		WithHome(t.TempDir()) // isolate from any real config file

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

// writeFakeAWSFailing creates a fake `aws` that mimics a failing command.
// Returns the directory containing it (to prepend to PATH).
func writeFakeAWSFailing(t *testing.T, exitCode int) string {
	t.Helper()
	return writeFakeTool(t, "aws", fakeToolConfig{
		Stderr:   []string{"aws: error: simulated failure"},
		ExitCode: exitCode,
	})
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
	dir := writeFakeTool(t, "aws", fakeToolConfig{Stdout: []string{"ARGS:{args}"}})

	for _, args := range [][]string{{"--help"}, {"-h"}, {"s3", "--help"}, {"help"}, {"s3", "help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			e := env.With(env.DisableEvents, "1").
				With("PATH", dir).
				WithHome(t.TempDir()).
				With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

			cmdArgs := append([]string{"aws"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)

			snap.Match(t, sanitizeOutput(stdout))
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
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

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
	_, err := dockerClient.ImageTag(ctx, client.ImageTagOptions{Source: testImage, Target: fakeImage})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = dockerClient.ImageRemove(context.Background(), fakeImage, client.ImageRemoveOptions{})
	})

	startExternalContainer(t, ctx, fakeImage, "localstack-main", "4566")

	fakeDir := writeFakeAWS(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws should work with externally-named container: %s", stderr)
	assert.Contains(t, stdout, "ENDPOINT:http://")
}

// writeSlowFakeAWS creates a fake `aws` that sleeps for the given duration
// before printing, so the spinner has time to render in PTY-based tests.
func writeSlowFakeAWS(t *testing.T, sleepSeconds int) string {
	t.Helper()
	return writeFakeTool(t, "aws", fakeToolConfig{
		SleepSeconds: sleepSeconds,
		Shift:        2,
		Stdout:       []string{"ENDPOINT:{arg2}", "ARGS:{args}"},
	})
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	out, err := runLstkInPTY(t, ctx, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.Contains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	assert.NotContains(t, out, "--profile")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	out, err := runLstkInPTY(t, ctx, e, "--non-interactive", "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.NotContains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	assert.NotContains(t, out, "--profile")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	out, err := runLstkInPTY(t, ctx, e, "aws", "s3", "ls")
	require.NoError(t, err, "lstk aws failed: %s", out)

	assert.NotContains(t, out, "Loading service")
	assert.Contains(t, out, "ARGS:s3 ls")
	// lstk selects the profile via AWS_PROFILE, never a --profile argument.
	assert.NotContains(t, out, "--profile")
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

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(homeDir)

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "aws", "s3", "ls")
	require.NoError(t, err)
	assert.NotContains(t, stdout, "lstk setup aws")
}
