package integration_test

import (
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// writeFakeSAM creates a stub `sam` that answers `--version` with the given
// version string and, for any other invocation, echoes its args and the AWS
// environment it was given so tests can assert what lstk injected/stripped.
func writeFakeSAM(t *testing.T, version string) string {
	t.Helper()
	return writeFakeTool(t, "sam", fakeToolConfig{
		Cases: []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"SAM CLI, version " + version}}},
		Stdout: []string{
			"ARGS:{args}",
			"ENV_AWS_ENDPOINT_URL={env:AWS_ENDPOINT_URL}",
			"ENV_AWS_ENDPOINT_URL_S3={env:AWS_ENDPOINT_URL_S3:-<unset>}",
			"ENV_AWS_REGION={env:AWS_REGION}",
			"ENV_AWS_DEFAULT_REGION={env:AWS_DEFAULT_REGION}",
			"ENV_AWS_ACCESS_KEY_ID={env:AWS_ACCESS_KEY_ID}",
			"ENV_AWS_SECRET_ACCESS_KEY={env:AWS_SECRET_ACCESS_KEY}",
			"ENV_AWS_PROFILE={env:AWS_PROFILE:-<unset>}",
			"ENV_AWS_DEFAULT_PROFILE={env:AWS_DEFAULT_PROFILE:-<unset>}",
			"ENV_AWS_SESSION_TOKEN={env:AWS_SESSION_TOKEN:-<unset>}",
		},
	})
}

// writeFakeSAMExit creates a stub `sam` reporting a supported version but exiting
// with the given code for any real subcommand.
func writeFakeSAMExit(t *testing.T, code int) string {
	t.Helper()
	return writeFakeTool(t, "sam", fakeToolConfig{
		Cases:    []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"SAM CLI, version 1.95.0"}}},
		Stderr:   []string{"sam: simulated failure"},
		ExitCode: code,
	})
}

// forwards args. `build` is offline, so no emulator/Docker is required.
func TestSAMForwardsArgs(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	require.Contains(t, stdout, "ARGS:build")
}

// propagates the sam exit code.
func TestSAMPropagatesExitCode(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAMExit(t, 7)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	require.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 7, err)
}

// the subprocess gets the LocalStack-pointing env (region in AWS_DEFAULT_REGION,
// custom --account in AWS_ACCESS_KEY_ID, no S3 endpoint), and ambient AWS config
// that could redirect at real AWS is stripped.
func TestSAMInjectsCleanAWSEnv(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()).
		With(env.Key("AWS_PROFILE"), "my-real-profile").
		With(env.Key("AWS_DEFAULT_PROFILE"), "other").
		With(env.Key("AWS_SESSION_TOKEN"), "realtoken")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--region", "eu-west-1", "--account", "111111111111", "build")
	require.NoError(t, err, "stderr: %s", stderr)

	// The snapshot pins the full env contract: endpoint scheme+port (host is
	// DNS-dependent, masked), both region vars, --account in the access key
	// (Terraform model), no S3-specific endpoint, and ambient AWS config
	// (profile/session token) stripped to <unset>.
	snap.Match(t, sanitizeOutput(stdout))
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
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
				"sam", "--region", "us-west-2", sub)
			require.NoError(t, err, "stderr: %s", stderr)

			require.Contains(t, stdout, "ARGS:"+sub)
			require.NotContains(t, stdout, "--region")
		})
	}
}

// DEVX-1002 — --help (and -h) never require the emulator, even for an
// AWS-contacting subcommand, and are forwarded to sam untouched.
func TestSAMHelpNoEmulator(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"deploy", "--help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeSAM(t, "1.95.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

			cmdArgs := append([]string{"sam"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)
			require.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
		})
	}
}

// a too-old sam fails before the command runs.
func TestSAMVersionTooOld(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.94.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	require.Contains(t, stderr+stdout, "1.95.0")
	// sam was never run for real.
	require.NotContains(t, stdout, "ARGS:build")
}

// a missing sam binary yields the install error.
func TestSAMMissingBinary(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.Error(t, err)
	require.Contains(t, stderr+stdout, "not found in PATH")
}

// --account is supported for sam (unlike cdk): a valid 12-digit value is
// accepted and reaches the subprocess as AWS_ACCESS_KEY_ID.
func TestSAMAccountSupported(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--account", "123456789012", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	require.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=123456789012")
}

// an invalid --account value is rejected at the command boundary before sam runs.
func TestSAMInvalidAccountRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--account", "12345", "build")
	require.Error(t, err)
	require.Contains(t, stderr+stdout, "12-digit")
	require.NotContains(t, stdout, "ARGS:build")
}

// flags after the subcommand are forwarded to sam unchanged.
func TestSAMFlagsAfterActionAreForwarded(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "build", "--region", "us-west-2")
	require.NoError(t, err, "stderr: %s", stderr)
	require.Contains(t, stdout, "ARGS:build --region us-west-2")
}

// a flag before the subcommand is rejected with a clear message.
func TestSAMFlagBeforeSubcommandRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--account", "111111111111", "sam", "build")
	require.Error(t, err)
	require.Contains(t, stderr+stdout, "must appear after the sam subcommand")
}

// LSTK_SAM_CMD selects the binary to invoke.
func TestSAMHonorsLstkSamCmd(t *testing.T) {
	t.Parallel()
	dir := writeFakeTool(t, "mysam", fakeToolConfig{
		Cases:  []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"SAM CLI, version 1.95.0"}}},
		Stdout: []string{"MYSAM:{args}"},
	})
	e := env.With(env.DisableEvents, "1").With("PATH", dir).WithHome(t.TempDir()).
		With(env.Key("LSTK_SAM_CMD"), "mysam")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "build")
	require.NoError(t, err, "stderr: %s", stderr)
	require.Contains(t, stdout, "MYSAM:build")
}

// an AWS-contacting command with no running emulator fails with "not running"
// and does not invoke sam.
func TestSAMFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "sam", "deploy")
	require.Error(t, err)
	require.Contains(t, stdout, "is not running")
	require.Contains(t, stdout, "Start LocalStack:")
	require.NotContains(t, stdout, "ARGS:deploy")
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
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "sam", "deploy")
	require.Error(t, err)
	require.Contains(t, stdout, "requires the")
	require.Contains(t, stdout, "Snowflake")
	require.NotContains(t, stdout, "ARGS:deploy")
}

// The region must reach SAM on its command line, not only through AWS_REGION.
// SAM injects samconfig.toml values as if typed on the command line, so a
// `region` key there outranks every environment variable — which silently
// defeated lstk's --region while --account (which has no samconfig.toml
// equivalent) kept working.
func TestSAMPutsSelectedRegionOnTheCommandLine(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "sam", "--region", "ap-northeast-1", "--account", "121212122323", "deploy")
	require.NoError(t, err, "stderr: %s", stderr)

	require.Contains(t, stdout, "--region ap-northeast-1", "the region must be on sam's command line")
	require.Contains(t, stdout, "ENV_AWS_REGION=ap-northeast-1")
	require.Contains(t, stdout, "ENV_AWS_ACCESS_KEY_ID=121212122323")
}

// Without lstk's --region there is nothing to assert over samconfig.toml, so
// lstk must not put one on the command line: injecting the us-east-1 default
// would override the region of every project that configured its own.
func TestSAMDoesNotInjectRegionWhenNotSelected(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "sam", "deploy")
	require.NoError(t, err, "stderr: %s", stderr)

	require.NotContains(t, stdout, "--region")
	// The environment still carries the default, as before.
	require.Contains(t, stdout, "ENV_AWS_REGION=us-east-1")
}

// An ambient AWS_REGION is used but is not a selection: it is commonly exported
// globally for real-AWS work and must not start overriding samconfig.toml.
func TestSAMDoesNotInjectAmbientRegion(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With(env.Key("AWS_REGION"), "eu-west-1")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "sam", "deploy")
	require.NoError(t, err, "stderr: %s", stderr)

	require.NotContains(t, stdout, "--region")
	require.Contains(t, stdout, "ENV_AWS_REGION=eu-west-1")
}

// Offline subcommands never get the flag: `init` and `docs` reject --region
// outright, and an offline command contacts nothing that needs a region.
func TestSAMDoesNotInjectRegionForOfflineSubcommand(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"sam", "--region", "ap-northeast-1", "build")
	require.NoError(t, err, "stderr: %s", stderr)

	require.Contains(t, stdout, "ARGS:build")
	require.NotContains(t, stdout, "--region")
	require.Contains(t, stdout, "ENV_AWS_REGION=ap-northeast-1")
}

// A --region the user addressed to sam directly wins: appending lstk's after it
// would silently outrank it.
func TestSAMLeavesUserSuppliedRegionFlagAlone(t *testing.T) {
	t.Parallel()
	srv := awsHealthServer(t)
	fakeDir := writeFakeSAM(t, "1.95.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--endpoint-url", srv.URL, "sam", "--region", "ap-northeast-1", "deploy", "--region", "us-west-1")
	require.NoError(t, err, "stderr: %s", stderr)

	// Pinned to the whole ARGS line: lstk must not append a second --region.
	require.Contains(t, stdout, "ARGS:deploy --region us-west-1\n")
	// The environment still carries lstk's resolved region, as it always has;
	// sam's own command-line flag is what outranks it.
	require.Contains(t, stdout, "ENV_AWS_REGION=ap-northeast-1")
}
