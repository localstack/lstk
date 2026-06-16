package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for `lstk sam` that exercise the real AWS SAM CLI against a
// real LocalStack container (see localstack_test.go for the shared bring-up
// helpers) — unlike sam_cmd_test.go, which uses a stub sam. They are gated on
// Docker + a real sam binary + an auth token (CI installs sam on the Linux
// shards and provides the token; otherwise they skip).
//
// A successful `sam deploy` against LocalStack proves the full path: lstk
// injected AWS_ENDPOINT_URL + mock creds + AWS_DEFAULT_REGION, SAM routed
// CloudFormation and the S3 artifact upload (--resolve-s3) through them, and
// LocalStack served the calls. No `sam build` is run: deploy packages the
// CodeUri directly (zipping ./src), which needs no language toolchain.

func requireSAM(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sam"); err != nil {
		t.Skip("sam binary not found on PATH")
	}
}

// copySAMSample copies a SAM sample project from test-samples/iac/sam into a
// fresh temp dir. Tests run there so .aws-sam/samconfig.toml never touch the
// tracked tree.
func copySAMSample(t *testing.T, name string) string {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join("test-samples", "iac", "sam", name)
	require.NoError(t, os.CopyFS(work, os.DirFS(src)))
	return work
}

// runSAM runs `lstk sam <args>` and returns stdout, stderr, err.
func runSAM(t *testing.T, ctx context.Context, work string, e env.Environ, args ...string) (string, string, error) {
	t.Helper()
	return runLstk(t, ctx, work, e, append([]string{"sam"}, args...)...)
}

// samE2EEnv inherits the real PATH (so the real sam is found) with an isolated
// HOME.
func samE2EEnv(t *testing.T) env.Environ {
	t.Helper()
	return env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
}

// `sam validate` succeeds offline (no running emulator required).
func TestSAME2EValidateOffline(t *testing.T) {
	requireSAM(t)

	ctx := testContext(t)
	work := copySAMSample(t, "hello")
	e := samE2EEnv(t)

	_, stderr, err := runSAM(t, ctx, work, e, "validate", "--lint")
	require.NoError(t, err, "sam validate stderr: %s", stderr)
}

// `sam deploy` of a single function succeeds against LocalStack, and `sam delete`
// tears it down. Exercises the --resolve-s3 artifact upload through the injected
// endpoint.
func TestSAME2EDeployDelete(t *testing.T) {
	requireDocker(t)
	requireSAM(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copySAMSample(t, "hello")
	e := samE2EEnv(t)
	const stack = "lstk-sam-e2e"

	_, stderr, err := runSAM(t, ctx, work, e, "deploy",
		"--stack-name", stack, "--resolve-s3", "--no-confirm-changeset",
		"--no-fail-on-empty-changeset", "--capabilities", "CAPABILITY_IAM", "--region", "us-east-1")
	require.NoError(t, err, "sam deploy stderr: %s", stderr)

	_, stderr, err = runSAM(t, ctx, work, e, "delete",
		"--stack-name", stack, "--no-prompts", "--region", "us-east-1")
	require.NoError(t, err, "sam delete stderr: %s", stderr)
}

// `sam deploy --account <id>` lands the stack under that LocalStack account: the
// deployed function's ARN (read back via `sam list stack-outputs`) carries the
// custom account id. Both the deploy and the read use the same --account.
func TestSAME2EDeployCustomAccount(t *testing.T) {
	requireDocker(t)
	requireSAM(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copySAMSample(t, "hello")
	e := samE2EEnv(t)
	const stack = "lstk-sam-e2e-acct"
	const account = "111111111111"

	_, stderr, err := runSAM(t, ctx, work, e, "--account", account, "deploy",
		"--stack-name", stack, "--resolve-s3", "--no-confirm-changeset",
		"--no-fail-on-empty-changeset", "--capabilities", "CAPABILITY_IAM", "--region", "us-east-1")
	require.NoError(t, err, "sam deploy stderr: %s", stderr)

	stdout, stderr, err := runSAM(t, ctx, work, e, "--account", account, "list", "stack-outputs",
		"--stack-name", stack, "--output", "json", "--region", "us-east-1")
	require.NoError(t, err, "sam list stack-outputs stderr: %s", stderr)
	assert.Contains(t, stdout, account, "function ARN should carry the custom account id")

	_, stderr, err = runSAM(t, ctx, work, e, "--account", account, "delete",
		"--stack-name", stack, "--no-prompts", "--region", "us-east-1")
	require.NoError(t, err, "sam delete stderr: %s", stderr)
}
