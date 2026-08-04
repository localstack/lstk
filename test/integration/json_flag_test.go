package integration_test

import (
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Most built-in commands haven't opted into --json output yet (see
// docs/structured-output.md's Command Catalog), so every one of these tests
// exercises the rejection gate (requireJSONSupport in cmd/root.go), which
// itself renders as a JSON envelope on stdout (error.code = NOT_JSON_CAPABLE)
// since that's the one guaranteed-universal response to --json.
//
// TestJSONFlagRejectsUnannotatedBuiltinCommand, TestJSONFlagRejectsDefaultStartBehavior,
// TestJSONFlagDoesNotLaunchTUIOnPTY, TestJSONFlagBeforeCommandNameBooleanValues, and
// TestExtensionReceivesJSONFlagInContext were ported to
// test/e2e/tests/json-flag.test.ts, json-envelope.test.ts, and
// json-envelope.pty.test.ts and removed from here. The az sub-case of the two
// table-driven proxy tests below stays: reaching a real `az` invocation needs a
// completed `lstk setup azure`, which the TS suite does not attempt.

// proxyCase describes one proxy command's forwarding/rejection setup, shared
// across the before/after-command-name test tables below.
type proxyCase struct {
	name  string
	args  []string
	setup func(t *testing.T) (workDir string, environ []string)
}

func genericProxySetup(t *testing.T) (string, []string) {
	return t.TempDir(), env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())
}

func azProxySetup(t *testing.T) (string, []string) {
	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)
	return workDir, env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())
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
			combined := stdout + stderr
			require.Contains(t, combined, "not found in PATH", "--json should have been forwarded to the wrapped tool, not rejected by lstk")
			require.NotContains(t, combined, "is not able to provide output in JSON format")
		})

		t.Run(tc.name+"/json after the wrapped tool's own action", func(t *testing.T) {
			t.Parallel()
			workDir, environ := tc.setup(t)
			args := append(append([]string{tc.name}, tc.args...), "--json")
			stdout, stderr, err := runLstk(t, testContext(t), workDir, environ, args...)
			require.Error(t, err)
			combined := stdout + stderr
			require.Contains(t, combined, "not found in PATH", "--json should have been forwarded to the wrapped tool, not rejected by lstk")
			require.NotContains(t, combined, "is not able to provide output in JSON format")
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
			assert.Equal(t, tc.name, envelope.Command)
			require.NotNil(t, envelope.Error)
			assert.Equal(t, "NOT_JSON_CAPABLE", envelope.Error.Code)
			assert.Empty(t, stderr, "the rejection is rendered as JSON on stdout, not plain text on stderr")
		})
	}
}
