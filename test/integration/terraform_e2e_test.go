package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// End-to-end tests for `lstk terraform` that exercise the real terraform/tofu
// binary, a real AWS provider installed via `terraform init`, and a real
// LocalStack container (see localstack_test.go for the shared bring-up helpers)
// — unlike other tests, which use a stub terraform and an alpine
// stand-in. They are gated on Docker + a terraform/tofu binary + an auth token
// (CI installs the binaries and provides the token; otherwise they skip).
//
// Most cases run `terraform apply` of a single S3 bucket rather than the bare
// `terraform plan` from the task matrix: a create-only `plan` against empty
// state makes no API calls (and the provider skips credential/metadata checks),
// so it would never touch LocalStack. `apply` issues a real CreateBucket through
// the generated override, so a successful apply proves the endpoint override
// routed to LocalStack end-to-end.

// realLocalStackImage is lstk's default AWS emulator image; it activates against
// LOCALSTACK_AUTH_TOKEN, which CI provides as a secret.
const realLocalStackImage = "localstack/localstack-pro:latest"

func requireTerraform(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not found on PATH")
	}
}

func requireTofu(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu binary not found on PATH")
	}
}

// e2eEnv inherits the real PATH (so the real terraform/tofu is found) with an
// isolated HOME, and points terraform at a shared plugin cache so providers are
// downloaded once per shard rather than once per test.
func e2eEnv(t *testing.T) env.Environ {
	t.Helper()
	return env.With(env.DisableEvents, "1").
		With(env.Home, t.TempDir()).
		With(env.Key("TF_PLUGIN_CACHE_DIR"), tfPluginCacheDir(t)).
		With(env.Key("TF_IN_AUTOMATION"), "1")
}

func tfPluginCacheDir(t *testing.T) string {
	t.Helper()
	tfCacheOnce.Do(func() {
		d, err := os.MkdirTemp("", "lstk-tf-plugin-cache")
		require.NoError(t, err)
		tfCacheDir = d
	})
	return tfCacheDir
}

var (
	tfCacheOnce sync.Once
	tfCacheDir  string
)

func countString(s, sub string) int {
	return strings.Count(s, sub)
}

// copySample copies a Terraform sample project from test-samples/iac/terraform
// into a fresh temp dir and returns it. Tests run there (not in the committed
// sample) so terraform's .terraform/state and lstk's override never touch the
// tracked tree. The sample's sub-directory layout (e.g. modules/) is preserved.
func copySample(t *testing.T, name string) string {
	t.Helper()
	work := t.TempDir()
	src := filepath.Join("test-samples", "iac", "terraform", name)
	require.NoError(t, os.CopyFS(work, os.DirFS(src)))
	return work
}

// runTerraform runs `lstk terraform <args>` and returns stdout, stderr, err. It
// wraps runLstk so the e2e tests read in terraform terms rather than "run lstk"
// (which obscures that terraform is what's being driven). The underlying binary
// honors LSTK_TF_CMD (e.g. tofu) when set in e.
func runTerraform(t *testing.T, ctx context.Context, work string, e env.Environ, args ...string) (string, string, error) {
	t.Helper()
	return runLstk(t, ctx, work, e, append([]string{"terraform"}, args...)...)
}

// tfInit runs `lstk terraform init` (provider download) and fails the test on
// error.
func tfInit(t *testing.T, ctx context.Context, work string, e env.Environ) {
	t.Helper()
	_, stderr, err := runTerraform(t, ctx, work, e, "init", "-no-color")
	require.NoError(t, err, "terraform init failed: %s", stderr)
}

// 8.1 + 8.2 — top-level project: real init + apply against a real LocalStack,
// and the generated override is removed afterward. This also exercises the
// harness (8.1).
func TestTerraformE2ETopLevelProject(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	work := copySample(t, "single-bucket")
	e := e2eEnv(t)

	tfInit(t, ctx, work, e)

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply stderr: %s", stderr)

	// The generated override is cleaned up after the run completes.
	assert.NoFileExists(t, filepath.Join(work, tfOverrideFile))
}

// 8.4 — single aws provider block (no alias): exactly one override block. We
// confirm the single block via a DRY_RUN capture (deterministic, no network),
// then apply to confirm it works against LocalStack.
func TestTerraformE2ESingleProvider(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	work := copySample(t, "single-bucket")
	e := e2eEnv(t)
	tfInit(t, ctx, work, e)

	override := captureOverride(t, ctx, work, e)
	assert.Equal(t, 1, countString(override, `provider "aws" {`), "exactly one provider block")

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply stderr: %s", stderr)
}

// 8.5 — multiple aliased providers plus the default: one override block per
// alias, and apply through both providers succeeds against LocalStack.
func TestTerraformE2EMultipleAliases(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	work := copySample(t, "multiple-aliases")
	e := e2eEnv(t)
	tfInit(t, ctx, work, e)

	override := captureOverride(t, ctx, work, e)
	assert.Equal(t, 2, countString(override, `provider "aws" {`), "default + aliased provider blocks")
	assert.Contains(t, override, `alias = "west"`)

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply stderr: %s", stderr)
}

// 8.3 — recursive discovery: a provider block in a sub-directory is represented
// in the override, while provider blocks under .terraform are ignored. Uses a
// DRY_RUN capture against the real provider schema.
func TestTerraformE2ESubdirectoryDiscovery(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	// The sample has a root provider plus an aliased provider in modules/db.
	work := copySample(t, "submodule")
	// A provider block under .terraform (the provider/module cache) must be
	// ignored by discovery. It's created at runtime rather than committed,
	// since a fake .terraform dir in the repo would be confusing.
	cacheDir := filepath.Join(work, ".terraform", "extra")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheDir, "cached.tf"),
		[]byte("provider \"aws\" {\n  alias = \"should_be_ignored\"\n}\n"), 0644))
	e := e2eEnv(t)
	tfInit(t, ctx, work, e)

	override := captureOverride(t, ctx, work, e)
	assert.Contains(t, override, `alias = "replica"`, "sub-directory provider discovered")
	assert.NotContains(t, override, "should_be_ignored", ".terraform provider ignored")
}

// 8.6 — a provider block that explicitly sets an endpoint (e.g. FIPS) is
// overridden: the generated override points S3 at LocalStack, and apply
// succeeds (it would fail against the public FIPS endpoint with mock creds).
func TestTerraformE2EExplicitEndpointOverridden(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	work := copySample(t, "explicit-endpoint")
	e := e2eEnv(t)
	tfInit(t, ctx, work, e)

	override := captureOverride(t, ctx, work, e)
	assert.NotContains(t, override, "s3-fips.us-east-1.amazonaws.com", "user FIPS endpoint must not survive")
	assert.Contains(t, override, "localstack", "override S3 endpoint targets LocalStack")

	_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "apply must route to LocalStack, not FIPS; stderr: %s", stderr)
}

// 8.7 — provider version coverage: schema-based endpoint discovery works for
// both the oldest supported provider (4.0) and the latest.
func TestTerraformE2EProviderVersions(t *testing.T) {
	requireDocker(t)
	requireTerraform(t)

	for _, tc := range []struct {
		name   string
		sample string
	}{
		{"v4.0", "provider-v4"},
		{"latest", "single-bucket"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			token := requireAuthToken(t)
			cleanup()
			t.Cleanup(cleanup)
			ctx := testContext(t)
			startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

			work := copySample(t, tc.sample)
			e := e2eEnv(t)
			tfInit(t, ctx, work, e)

			// A successful apply requires schema discovery (endpoint keys) to
			// have worked for this provider version.
			_, stderr, err := runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
			require.NoError(t, err, "apply stderr: %s", stderr)
		})
	}
}

// 8.8 — the same top-level flow under OpenTofu (LSTK_TF_CMD=tofu), since
// behavior may differ subtly from terraform.
func TestTerraformE2ETofu(t *testing.T) {
	requireDocker(t)
	requireTofu(t)
	token := requireAuthToken(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startRealLocalStack(t, ctx, realLocalStackImage, containerName, token)

	work := copySample(t, "single-bucket")
	e := e2eEnv(t).With(env.Key("LSTK_TF_CMD"), "tofu")

	_, stderr, err := runTerraform(t, ctx, work, e, "init", "-no-color")
	require.NoError(t, err, "tofu init stderr: %s", stderr)

	_, stderr, err = runTerraform(t, ctx, work, e, "apply", "-auto-approve", "-no-color")
	require.NoError(t, err, "tofu apply stderr: %s", stderr)

	assert.NoFileExists(t, filepath.Join(work, tfOverrideFile))
}

// captureOverride runs the command with LSTK_TF_DRY_RUN=1 so the generated
// override file is written and left in place, then returns its contents.
func captureOverride(t *testing.T, ctx context.Context, work string, e env.Environ) string {
	t.Helper()
	_, stderr, err := runTerraform(t, ctx, work, e.With(env.Key("LSTK_TF_DRY_RUN"), "1"),
		"plan", "-no-color")
	require.NoError(t, err, "dry-run stderr: %s", stderr)
	content, err := os.ReadFile(filepath.Join(work, tfOverrideFile))
	require.NoError(t, err, "override file should exist after dry run")
	// Remove it so a subsequent real run doesn't trip the pre-existing-file guard.
	require.NoError(t, os.Remove(filepath.Join(work, tfOverrideFile)))
	return string(content)
}
