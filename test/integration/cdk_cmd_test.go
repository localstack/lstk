package integration_test

import (
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFakeCDK creates a stub `cdk` that answers `--version` with the given
// version string and, for any other invocation, echoes its args and the AWS
// environment it was given so tests can assert what lstk injected/stripped.
func writeFakeCDK(t *testing.T, version string) string {
	t.Helper()
	return writeFakeTool(t, "cdk", fakeToolConfig{
		Cases: []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{version}}},
		Stdout: []string{
			"ARGS:{args}",
			"ENV_AWS_ENDPOINT_URL={env:AWS_ENDPOINT_URL}",
			"ENV_AWS_ENDPOINT_URL_S3={env:AWS_ENDPOINT_URL_S3}",
			"ENV_AWS_REGION={env:AWS_REGION}",
			"ENV_AWS_ACCESS_KEY_ID={env:AWS_ACCESS_KEY_ID}",
			"ENV_AWS_SECRET_ACCESS_KEY={env:AWS_SECRET_ACCESS_KEY}",
			"ENV_AWS_PROFILE={env:AWS_PROFILE:-<unset>}",
			"ENV_AWS_DEFAULT_PROFILE={env:AWS_DEFAULT_PROFILE:-<unset>}",
			"ENV_AWS_SESSION_TOKEN={env:AWS_SESSION_TOKEN:-<unset>}",
		},
	})
}

// writeFakeCDKExit creates a stub `cdk` reporting a supported version but exiting
// with the given code for any real subcommand.
func writeFakeCDKExit(t *testing.T, code int) string {
	t.Helper()
	return writeFakeTool(t, "cdk", fakeToolConfig{
		Cases:    []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"2.177.0"}}},
		Stderr:   []string{"cdk: simulated failure"},
		ExitCode: code,
	})
}

// 7.1 — forwards args. `synth` is offline, so no emulator/Docker is required.
func TestCDKForwardsArgs(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDK(t, "2.177.0 (build abc123)")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "synth")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.1 — propagates the cdk exit code.
func TestCDKPropagatesExitCode(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDKExit(t, 7)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "synth")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stderr))
	requireExitCode(t, 7, err)
}

// 7.2 — the subprocess gets the LocalStack-pointing env, and ambient AWS config
// that could redirect at real AWS is stripped.
func TestCDKInjectsCleanAWSEnv(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDK(t, "2.177.0")
	// A 12-digit AWS_ACCESS_KEY_ID would make LocalStack resolve a custom
	// account; lstk must override it with "test" so CDK always uses the default
	// account 000000000000.
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()).
		With(env.Key("AWS_PROFILE"), "my-real-profile").
		With(env.Key("AWS_DEFAULT_PROFILE"), "other").
		With(env.Key("AWS_SESSION_TOKEN"), "realtoken").
		With(env.Key("AWS_ACCESS_KEY_ID"), "999999999999")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"cdk", "--region", "eu-west-1", "synth")
	require.NoError(t, err, "stderr: %s", stderr)

	// The snapshot pins the full env contract: both endpoints' scheme+port
	// (host is DNS-dependent, masked), the region, the forced "test" access
	// key (a real-looking ambient key must not select a custom account), and
	// ambient AWS config stripped to <unset>.
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.4 — offline subcommands run without a running emulator, even with a leading
// --region present (which is stripped from the forwarded args).
func TestCDKOfflineCommandsNoEmulator(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"init", "synth", "ls", "version", "doctor"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeCDK(t, "2.177.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
				"cdk", "--region", "us-west-2", sub)
			require.NoError(t, err, "stderr: %s", stderr)

			// The snapshot pins the forwarded args: the leading --region must
			// be stripped, not forwarded to cdk.
			snap.Match(t, sanitizeOutput(stdout))
		})
	}
}

// DEVX-1002 — --help/-h, and the bare "help" pseudo-subcommand, never require
// the emulator, even for an AWS-contacting subcommand, and are forwarded to
// cdk untouched.
func TestCDKHelpNoEmulator(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"deploy", "--help"}, {"help"}, {"deploy", "help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeCDK(t, "2.177.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

			cmdArgs := append([]string{"cdk"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)
			snap.Match(t, sanitizeOutput(stdout))
		})
	}
}

// 7.5 — a too-old cdk fails before the command runs.
func TestCDKVersionTooOld(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDK(t, "2.176.0 (build old)")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "synth")
	require.Error(t, err)
	// The snapshots pin the full version error; cdk was never run for real
	// (no ARGS line in either stream).
	snap.Match(t, sanitizeOutput(stderr))
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.5 — a missing cdk binary yields the install error.
func TestCDKMissingBinary(t *testing.T) {
	t.Parallel()
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "synth")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stderr))
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.6 — --account is not supported for cdk and is rejected at the command
// boundary (with any value, including a valid 12-digit one) before cdk runs.
func TestCDKAccountRejected(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"123456789012", "12345"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeCDK(t, "2.177.0")
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

			stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
				"cdk", "--account", value, "synth")
			require.Error(t, err)
			// The snapshots pin the rejection; cdk was never run (no ARGS line).
			snap.Match(t, sanitizeOutput(stderr))
			snap.Match(t, sanitizeOutput(stdout))
		})
	}
}

// 7.6 — flags after the subcommand are forwarded to cdk unchanged.
func TestCDKFlagsAfterActionAreForwarded(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"cdk", "synth", "--region", "us-west-2")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.6 — a flag before the subcommand is rejected with a clear message.
func TestCDKFlagBeforeSubcommandRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--account", "111111111111", "cdk", "synth")
	require.Error(t, err)
	snap.Match(t, sanitizeOutput(stderr))
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.7 — LSTK_CDK_CMD selects the binary to invoke.
func TestCDKHonorsLstkCdkCmd(t *testing.T) {
	t.Parallel()
	dir := writeFakeTool(t, "mycdk", fakeToolConfig{
		Cases:  []fakeToolCase{{Args: []string{"--version"}, Stdout: []string{"2.177.0"}}},
		Stdout: []string{"MYCDK:{args}"},
	})
	e := env.With(env.DisableEvents, "1").With("PATH", dir).WithHome(t.TempDir()).
		With(env.Key("LSTK_CDK_CMD"), "mycdk")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "synth")
	require.NoError(t, err, "stderr: %s", stderr)
	snap.Match(t, sanitizeOutput(stdout))
}

// 7.3 — an AWS-contacting command with no running emulator fails with "not
// running" and does not invoke cdk.
func TestCDKFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir()).
		With(env.LocalStackHost, deadLocalStackHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "cdk", "deploy")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.NotContains(t, stdout, "ARGS:deploy")
}

// 7.3 — an AWS-contacting command fails with an AWS-specific error naming the
// running non-AWS emulator, and does not invoke cdk.
func TestCDKRequiresAWSEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)
	startTestSnowflakeContainer(t, ctx)

	fakeDir := writeFakeCDK(t, "2.177.0")
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).WithHome(t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "cdk", "deploy")
	require.Error(t, err)
	assert.Contains(t, stdout, "requires the")
	assert.Contains(t, stdout, "Snowflake")
	assert.NotContains(t, stdout, "ARGS:deploy")
}
