package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for `lstk cdk` that exercise the real AWS CDK CLI against a
// real LocalStack container (see localstack_test.go for the shared bring-up
// helpers) — unlike cdk_cmd_test.go, which uses a stub cdk and an alpine
// stand-in. They are gated on Docker + a real cdk binary + npm + an auth token
// (CI installs cdk on the Linux shards and provides the token; otherwise they
// skip).
//
// A successful `cdk deploy` against LocalStack proves the full path: lstk
// injected AWS_ENDPOINT_URL/AWS_ENDPOINT_URL_S3 + mock creds, CDK routed
// CloudFormation and the S3 asset staging through them, and LocalStack served
// the calls. The S3 staging in particular validates the virtual-host S3
// endpoint (s3.localhost.localstack.cloud) lstk derives.

func requireCDK(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("cdk"); err != nil {
		t.Skip("cdk binary not found on PATH")
	}
}

func requireNpm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not found on PATH")
	}
}

// copyCDKSample copies a CDK sample project from test-samples/iac/cdk into a
// fresh temp dir. Tests run there so node_modules/cdk.out never touch the
// tracked tree.
func copyCDKSample(t *testing.T, name string) string {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join("test-samples", "iac", "cdk", name)
	require.NoError(t, os.CopyFS(work, os.DirFS(src)))
	return work
}

// npmInstall installs the sample's dependencies (aws-cdk-lib, constructs) into
// the copied work dir so the CDK app can synthesize. The sample pins
// aws-cdk-lib to an older exact version on purpose: a CDK CLI can read cloud
// assemblies from its own and older aws-cdk-lib schema versions but not newer
// ones, so pinning the library below the minimum supported CLI (2.177) keeps
// synth/deploy working against any CLI >= 2.177, including the latest CI installs.
func npmInstall(t *testing.T, ctx context.Context, work string, e env.Environ) {
	t.Helper()
	cmd := exec.CommandContext(ctx, "npm", "install", "--no-audit", "--no-fund", "--loglevel=error")
	cmd.Dir = work
	cmd.Env = e
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("npm install failed: %v\n%s", err, out)
	}
}

// runCDK runs `lstk cdk <args>` and returns stdout, stderr, err. Wrapping
// runLstk lets the e2e tests read in cdk terms.
func runCDK(t *testing.T, ctx context.Context, work string, e env.Environ, args ...string) (string, string, error) {
	t.Helper()
	return runLstk(t, ctx, work, e, append([]string{"cdk"}, args...)...)
}

// cdkE2EEnv inherits the real PATH (so the real cdk/node are found) with an
// isolated HOME.
func cdkE2EEnv(t *testing.T) env.Environ {
	t.Helper()
	return env.With(env.DisableEvents, "1").With(env.Home, t.TempDir())
}

// 8.4 — `cdk synth` succeeds offline (no running emulator required).
func TestCDKE2ESynthOffline(t *testing.T) {
	requireCDK(t)
	requireNpm(t)

	ctx := testContext(t)
	work := copyCDKSample(t, "single-bucket")
	e := cdkE2EEnv(t)
	npmInstall(t, ctx, work, e)

	_, stderr, err := runCDK(t, ctx, work, e, "synth")
	require.NoError(t, err, "cdk synth stderr: %s", stderr)
}

// 8.1 + 8.2 — `cdk bootstrap` succeeds against a real LocalStack (also
// exercises the bring-up harness).
func TestCDKE2EBootstrap(t *testing.T) {
	requireDocker(t)
	requireCDK(t)
	requireNpm(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copyCDKSample(t, "single-bucket")
	e := cdkE2EEnv(t)
	npmInstall(t, ctx, work, e)

	_, stderr, err := runCDK(t, ctx, work, e, "bootstrap")
	require.NoError(t, err, "cdk bootstrap stderr: %s", stderr)
}

// 8.3 — `cdk deploy` of a single bucket succeeds against LocalStack, and
// `cdk destroy` tears it down. Deploy requires a prior bootstrap.
func TestCDKE2EDeployDestroy(t *testing.T) {
	requireDocker(t)
	requireCDK(t)
	requireNpm(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copyCDKSample(t, "single-bucket")
	e := cdkE2EEnv(t)
	npmInstall(t, ctx, work, e)

	_, stderr, err := runCDK(t, ctx, work, e, "bootstrap")
	require.NoError(t, err, "cdk bootstrap stderr: %s", stderr)

	_, stderr, err = runCDK(t, ctx, work, e, "deploy", "--require-approval", "never")
	require.NoError(t, err, "cdk deploy stderr: %s", stderr)

	_, stderr, err = runCDK(t, ctx, work, e, "destroy", "--force")
	require.NoError(t, err, "cdk destroy stderr: %s", stderr)
}

// 8.6 + 8.7 — full Lambda round-trip against LocalStack. Unlike the single-bucket
// stack (whose small template may be passed inline to CloudFormation and so might
// never touch S3), the lambda-asset stack loads its code with Code.fromAsset,
// which forces CDK to zip the lambda/ dir and PutObject it to the bootstrap
// staging bucket at deploy time. That upload uses the S3 client configured from
// the AWS_ENDPOINT_URL_S3 value lstk injects, so a successful deploy proves the
// asset path routed through LocalStack — this is where AWS_ENDPOINT_URL_S3 is
// fully exercised. When the real aws CLI is present we additionally invoke the
// deployed function and assert its output, proving LocalStack provisioned it from
// the uploaded asset end-to-end. (LocalStack's own read of the asset is internal
// and does not use lstk's endpoint; the publish step is the part that does.)
func TestCDKE2ELambdaAssetDeployDestroy(t *testing.T) {
	requireDocker(t)
	requireCDK(t)
	requireNpm(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copyCDKSample(t, "lambda-asset")
	e := cdkE2EEnv(t)
	npmInstall(t, ctx, work, e)

	_, stderr, err := runCDK(t, ctx, work, e, "bootstrap")
	require.NoError(t, err, "cdk bootstrap stderr: %s", stderr)

	_, stderr, err = runCDK(t, ctx, work, e, "deploy", "--require-approval", "never")
	require.NoError(t, err, "cdk deploy stderr: %s", stderr)

	// Strongest proof when available: invoke the function (provisioned from the
	// uploaded asset) and assert the handler's response. lstk aws shells out to
	// the real aws CLI, so only run this when it is installed; the deploy above
	// already proves the asset was published across AWS_ENDPOINT_URL_S3.
	if _, lookErr := exec.LookPath("aws"); lookErr == nil {
		outFile := filepath.Join(work, "invoke-out.json")
		_, stderr, err = runLstk(t, ctx, work, e, "aws", "lambda", "invoke",
			"--function-name", "lstk-cdk-e2e-fn", outFile)
		require.NoError(t, err, "lstk aws lambda invoke stderr: %s", stderr)

		out, readErr := os.ReadFile(outFile)
		require.NoError(t, readErr)
		require.Contains(t, string(out), "ok from lstk cdk lambda", "lambda response: %s", out)
	} else {
		t.Log("aws CLI not on PATH; skipping lambda invoke verification (deploy already exercised the S3 asset path)")
	}

	_, stderr, err = runCDK(t, ctx, work, e, "destroy", "--force")
	require.NoError(t, err, "cdk destroy stderr: %s", stderr)
}
