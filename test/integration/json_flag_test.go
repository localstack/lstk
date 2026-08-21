package integration_test

import (
	"testing"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Most built-in commands haven't opted into --json output yet (see
// docs/structured-output.md's Command Catalog), so every one of these tests
// exercises the rejection gate (requireJSONSupport in cmd/root.go), which
// itself renders as a JSON envelope on stdout (error.code = NOT_JSON_CAPABLE)
// since that's the one guaranteed-universal response to --json.

func TestJSONFlagRejectsUnannotatedBuiltinCommand(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "status", "--json")
	requireExitCode(t, 1, err)
	decodeEnvelope(t, stdout)
	snap.MatchJSON(t, []byte(stdout))
	assert.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
}

// `start` and the bare invocation are JSON-capable; see start_json_test.go.

func TestJSONFlagDoesNotLaunchTUIOnPTY(t *testing.T) {
	t.Parallel()

	// Without an unreachable runtime, a machine with a live daemon gets past the
	// runtime check into AUTH_REQUIRED (exit 4) instead of exit 1.
	e := append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost)
	out, err := runLstkInPTY(t, testContext(t), e, "start", "--json")
	requireExitCode(t, 1, err)
	require.Contains(t, out, "start")
	// If the TUI had launched, it would have shown the auth prompt (start with
	// no auth token requires interactive login) rather than exiting immediately.
	require.NotContains(t, out, "Press any key")
}

// proxyCase describes one proxy command's forwarding/rejection setup, shared
// across the before/after-command-name test tables below.
type proxyCase struct {
	name  string
	args  []string
	setup func(t *testing.T) (workDir string, environ []string)
}

func genericProxySetup(t *testing.T) (string, []string) {
	return t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())
}

func azProxySetup(t *testing.T) (string, []string) {
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)
	return workDir, env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir())
}

func proxyCases() []proxyCase {
	return []proxyCase{
		{name: "aws", args: []string{"s3", "ls"}, setup: genericProxySetup},
		{name: "terraform", args: []string{"version"}, setup: genericProxySetup},
		{name: "cdk", args: []string{"synth"}, setup: genericProxySetup},
		{name: "sam", args: []string{"build"}, setup: genericProxySetup},
		{name: "az", args: []string{"group", "list"}, setup: azProxySetup},
	}
}

// TestJSONFlagProxyCommandsForwardJSON covers all five proxy commands
// (aws/terraform/cdk/sam/az) with one parametrized test: --json is never
// recognized or intercepted from the command name onward — it always reaches
// the wrapped tool untouched, whether typed immediately after the command name
// or after the wrapped tool's own action (see spec.md "Proxy commands forward
// --json from the command name onward"). This is what lets Terraform's own
// real -json/--json flag on plan/apply/show keep working.
//
// Each case reuses the exact "<tool> CLI not found in PATH" setup already
// established by TestAWSCommandFailsWhenAWSCLINotInstalled /
// TestTerraformMissingBinary / TestCDKMissingBinary / TestSAMMissingBinary /
// TestAzCommandFailsWhenAzureCLINotInstalled: an empty PATH means the wrapped
// binary is never found, which only happens if lstk actually attempted to
// invoke it — proving --json did not stop the invocation.
func TestJSONFlagProxyCommandsForwardJSON(t *testing.T) {
	t.Parallel()

	for _, tc := range proxyCases() {
		t.Run(tc.name+"/json immediately after command name", func(t *testing.T) {
			t.Parallel()
			workDir, environ := tc.setup(t)
			args := append([]string{tc.name, "--json"}, tc.args...)
			stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, args...)
			require.Error(t, err)
			// The snapshots pin the wrapped tool's missing-binary error —
			// proof --json was forwarded, not rejected by lstk.
			snap.Match(t, sanitizeOutput(stdout))
			snap.Match(t, sanitizeOutput(stderr))
		})

		t.Run(tc.name+"/json after the wrapped tool's own action", func(t *testing.T) {
			t.Parallel()
			workDir, environ := tc.setup(t)
			args := append(append([]string{tc.name}, tc.args...), "--json")
			stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, args...)
			require.Error(t, err)
			snap.Match(t, sanitizeOutput(stdout))
			snap.Match(t, sanitizeOutput(stderr))
		})
	}
}

// TestJSONFlagProxyCommandsRejectBeforeCommandName covers all five proxy
// commands with one parametrized test: --json typed before the proxy
// command's own name sits in the same flag-namespace slot --non-interactive/
// --config already occupy there, so lstk rejects it exactly like an
// unsupported built-in command instead of silently forwarding it to a wrapped
// tool that likely doesn't understand it (see spec.md "Proxy commands reject
// --json before the command name").
func TestJSONFlagProxyCommandsRejectBeforeCommandName(t *testing.T) {
	t.Parallel()

	for _, tc := range proxyCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workDir, environ := tc.setup(t)
			args := append([]string{"--json", tc.name}, tc.args...)
			stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, args...)
			requireExitCode(t, 1, err)
			decodeEnvelope(t, stdout)
			snap.MatchJSON(t, []byte(stdout))
			assert.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
		})
	}
}

// TestJSONFlagBeforeCommandNameBooleanValues exercises the boolean-aware
// parsing jsonPrecedesCommandName applies (mirroring stripGlobalFlags's
// existing --non-interactive=<value> handling), using aws as a representative
// proxy command since it has no leading IaC-flag tier of its own to interact
// with.
func TestJSONFlagBeforeCommandNameBooleanValues(t *testing.T) {
	t.Parallel()

	t.Run("--json=true before the command name is rejected", func(t *testing.T) {
		t.Parallel()
		stdout, _, err := runLstk(t, testContext(t), t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir()), "--json=true", "aws", "s3", "ls")
		requireExitCode(t, 1, err)
		decodeEnvelope(t, stdout)
		snap.MatchJSON(t, []byte(stdout))
	})

	t.Run("--json=false before the command name is not rejected", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir()), "--json=false", "aws", "s3", "ls")
		require.Error(t, err)
		// The snapshots pin the wrapped tool's missing-binary error (proof the
		// tool was invoked, i.e. --json=false was not rejected) with no
		// NOT_JSON_CAPABLE envelope anywhere.
		snap.Match(t, sanitizeOutput(stdout))
		snap.Match(t, sanitizeOutput(stderr))
	})

	t.Run("a malformed value before the command name is rejected", func(t *testing.T) {
		t.Parallel()
		stdout, _, err := runLstk(t, testContext(t), t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir()), "--json=notabool", "aws", "s3", "ls")
		requireExitCode(t, 1, err)
		decodeEnvelope(t, stdout)
		snap.MatchJSON(t, []byte(stdout))
	})
}

func TestExtensionReceivesJSONFlagInContext(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "hello")
	tmpHome := t.TempDir()
	environ := envWithPath(tmpHome, extDir)

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "--json", "hello", "--foo")
	require.NoError(t, err, stderr)
	// --json is consumed by lstk and conveyed via env, not forwarded; it also
	// forces non-interactive rendering, so the extension sees that too.
	snap.Match(t, sanitizeOutput(stdout))

	stdoutDefault, stderrDefault, errDefault := runLstk(t, testContext(t), t.TempDir(), environ, "hello", "--foo")
	require.NoError(t, errDefault, stderrDefault)
	require.Contains(t, stdoutDefault, "JSON=false")
}
