package integration_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for the S3 state backend and terraform_remote_state support.
// They drive a real terraform/tofu against a real LocalStack (see
// terraform_e2e_test.go for the shared helpers): `init` now requires the
// emulator and provisions the state bucket/lock table when an S3 backend is
// declared, then `apply` reads and writes state in LocalStack.

// tfStateList runs `lstk terraform state list` and returns its stdout. A
// successful read proves the state round-tripped through the LocalStack backend.
func tfStateList(t *testing.T, ctx context.Context, work string, e env.Environ) string {
	t.Helper()
	stdout, stderr, err := runTerraform(t, ctx, work, e, "state", "list")
	require.NoError(t, err, "state list failed: %s", stderr)
	return stdout
}

// 10.1 — an S3 backend: init provisions the state bucket and redirects the
// backend, apply writes state to LocalStack, and the state is read back from
// LocalStack. The generated override is removed afterward.
func TestTerraformE2ES3Backend(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copySample(t, "s3-backend")
	e := e2eEnv(t)

	// init must provision the bucket (fresh LocalStack has none) and configure
	// the redirected backend against LocalStack.
	tfInit(t, ctx, work, e)

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply stderr: %s", stderr)

	// State written to and read back from the LocalStack bucket.
	assert.Contains(t, tfStateList(t, ctx, work, e), "aws_s3_bucket.b")

	assert.NoFileExists(t, filepath.Join(work, tfOverrideFile))
}

// 10.2 — an S3 backend with DynamoDB locking: lstk creates the lock table, and
// apply (which acquires and releases a DynamoDB lock) succeeds — proving the
// table was provisioned and is usable.
func TestTerraformE2ES3BackendLocking(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copySample(t, "s3-backend-locking")
	e := e2eEnv(t)

	tfInit(t, ctx, work, e)

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply with DynamoDB locking stderr: %s", stderr)

	assert.Contains(t, tfStateList(t, ctx, work, e), "aws_s3_bucket.b")
}

// 10.3 — terraform_remote_state: a producer stack writes state (with an output)
// to LocalStack; a consumer stack reads that state through a redirected
// terraform_remote_state data source and exposes the producer's output.
func TestTerraformE2ERemoteState(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	// Producer: apply so its state (and the bucket_name output) lands in LocalStack.
	producer := copySample(t, "remote-state/producer")
	e := e2eEnv(t)
	tfInit(t, ctx, producer, e)
	_, stderr, err := runTerraform(t, ctx, producer, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "producer apply stderr: %s", stderr)

	// Consumer: reads the producer's remote state from LocalStack.
	consumer := copySample(t, "remote-state/consumer")
	tfInit(t, ctx, consumer, e)
	_, stderr, err = runTerraform(t, ctx, consumer, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "consumer apply stderr: %s", stderr)

	stdout, stderr, err := runTerraform(t, ctx, consumer, e, "output", "-raw", "producer_bucket")
	require.NoError(t, err, "consumer output stderr: %s", stderr)
	assert.Contains(t, stdout, "lstk-e2e-remote-producer", "consumer read the producer's output from LocalStack remote state")
}

// 10.4 — the S3 backend flow under OpenTofu (LSTK_TF_CMD=tofu), covering the
// version-aware endpoint form on a different toolchain.
func TestTerraformE2ES3BackendTofu(t *testing.T) {
	requireDocker(t)
	requireTofu(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, token)

	work := copySample(t, "s3-backend")
	e := e2eEnv(t).With(env.Key("LSTK_TF_CMD"), "tofu")

	tfInit(t, ctx, work, e)

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "tofu apply stderr: %s", stderr)

	assert.Contains(t, tfStateList(t, ctx, work, e), "aws_s3_bucket.b")
}

// 10.5 — a configuration with no S3 backend still behaves as before: `init`
// passes through and does NOT require a running emulator (no LocalStack is
// started here, yet init succeeds).
func TestTerraformE2EInitNoBackendNoEmulator(t *testing.T) {
	requireTerraform(t)
	ctx := testContext(t)

	work := copySample(t, "single-bucket")
	e := e2eEnv(t)

	_, stderr, err := runTerraform(t, ctx, work, e, "init", "-no-color")
	require.NoError(t, err, "backend-less init must not require an emulator; stderr: %s", stderr)

	// No override is generated for a backend-less init.
	assert.NoFileExists(t, filepath.Join(work, tfOverrideFile))
}
