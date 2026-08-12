package integration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
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
	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "status", envelope.Command)
	must.Eq(t, "error", envelope.Status)
	must.NotNil(t, envelope.Error)
	must.Eq(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
	must.Contains(t, envelope.Error.Message, "status")
	must.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
}

func TestJSONFlagRejectsDefaultStartBehavior(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--json")
	requireExitCode(t, 1, err)
	envelope := decodeEnvelope(t, stdout)
	must.Eq(t, "start", envelope.Command)
	must.Eq(t, "error", envelope.Status)
	must.NotNil(t, envelope.Error)
	must.Eq(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
	must.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
}

func TestJSONFlagDoesNotLaunchTUIOnPTY(t *testing.T) {
	t.Parallel()

	out, err := runLstkInPTY(t, testContext(t), testEnvWithHome(t.TempDir(), ""), "start", "--json")
	requireExitCode(t, 1, err)
	must.Contains(t, out, "start")
	// If the TUI had launched, it would have shown the auth prompt (start with
	// no auth token requires interactive login) rather than exiting immediately.
	must.NotContains(t, out, "Press any key")
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
			must.Error(t, err)
			combined := stdout + stderr
			must.Contains(t, combined, "not found in PATH", "--json should have been forwarded to the wrapped tool, not rejected by lstk")
			must.NotContains(t, combined, "is not able to provide output in JSON format")
		})

		t.Run(tc.name+"/json after the wrapped tool's own action", func(t *testing.T) {
			t.Parallel()
			workDir, environ := tc.setup(t)
			args := append(append([]string{tc.name}, tc.args...), "--json")
			stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, args...)
			must.Error(t, err)
			combined := stdout + stderr
			must.Contains(t, combined, "not found in PATH", "--json should have been forwarded to the wrapped tool, not rejected by lstk")
			must.NotContains(t, combined, "is not able to provide output in JSON format")
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
			envelope := decodeEnvelope(t, stdout)
			must.Eq(t, tc.name, envelope.Command)
			must.NotNil(t, envelope.Error)
			must.Eq(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
			must.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
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
		envelope := decodeEnvelope(t, stdout)
		must.Eq(t, "aws", envelope.Command)
		must.NotNil(t, envelope.Error)
		must.Eq(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
	})

	t.Run("--json=false before the command name is not rejected", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir()), "--json=false", "aws", "s3", "ls")
		must.Error(t, err)
		combined := stdout + stderr
		must.Contains(t, combined, "not found in PATH", "the wrapped tool should have run (and failed for its own, unrelated reason)")
		must.NotContains(t, combined, "is not able to provide output in JSON format")
	})

	t.Run("a malformed value before the command name is rejected", func(t *testing.T) {
		t.Parallel()
		stdout, _, err := runLstk(t, testContext(t), t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).WithHome(t.TempDir()), "--json=notabool", "aws", "s3", "ls")
		requireExitCode(t, 1, err)
		envelope := decodeEnvelope(t, stdout)
		must.Eq(t, "aws", envelope.Command)
		must.NotNil(t, envelope.Error)
		must.Eq(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
	})
}

func TestExtensionReceivesJSONFlagInContext(t *testing.T) {
	t.Parallel()
	extDir := t.TempDir()
	installExtension(t, extDir, "hello")
	tmpHome := t.TempDir()
	// Strip any ambient auth token: it would add an environment-dependent
	// AUTH_TOKEN line to the snapshotted context echo (and put a real secret
	// in the failure diff).
	environ := slices.DeleteFunc(envWithPath(tmpHome, extDir), func(kv string) bool {
		return strings.HasPrefix(kv, string(env.AuthToken)+"=")
	})

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), environ, "--json", "hello", "--foo")
	must.NoError(t, err, stderr)
	// --json is consumed by lstk and conveyed via env, not forwarded; it also
	// forces non-interactive rendering, so the extension sees that too.
	snap.Match(t, sanitizeOutput(stdout))

	stdoutDefault, stderrDefault, errDefault := runLstk(t, testContext(t), t.TempDir(), environ, "hello", "--foo")
	must.NoError(t, errDefault, stderrDefault)
	must.Contains(t, stdoutDefault, "JSON=false")
}
