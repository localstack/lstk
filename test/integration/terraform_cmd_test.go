package integration_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tfOverrideFile = "localstack_providers_override.tf"

// awsSchemaJSON is a minimal `terraform providers schema -json` payload exposing
// a couple of endpoint keys for the AWS provider, enough for endpoint discovery.
const awsSchemaJSON = `{"provider_schemas":{"registry.terraform.io/hashicorp/aws":{"provider":{"block":{"block_types":{"endpoints":{"block":{"attributes":{"s3":{"type":"string"},"sqs":{"type":"string"}}}}}}}}}}`

// writeFakeTerraform creates a stub `terraform` that answers `providers schema
// -json` with awsSchemaJSON and echoes its args for any other invocation.
func writeFakeTerraform(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()
	// On a proxied command the stub echoes the generated override file (read with
	// shell builtins only, since PATH is restricted to the stub dir) so tests can
	// confirm it existed with schema-derived keys during the run.
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "providers" ] && [ "$2" = "schema" ]; then
  printf '%%s' '%s'
  exit 0
fi
echo "ARGS:$*"
if [ -f localstack_providers_override.tf ]; then
  while IFS= read -r line; do echo "TF> $line"; done < localstack_providers_override.tf
fi
`, awsSchemaJSON)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0755))
	return dir
}

// writeFakeTerraformFailingSchema creates a stub whose `providers schema` call
// fails (as it would before `terraform init`), echoing args otherwise.
func writeFakeTerraformFailingSchema(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "providers" ] && [ "$2" = "schema" ]; then
  echo "Error: required providers not installed" >&2
  exit 1
fi
echo "ARGS:$*"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0755))
	return dir
}

// writeFakeTerraformExit creates a stub that exits with the given code.
func writeFakeTerraformExit(t *testing.T, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake terraform script not supported on Windows")
	}
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
echo "terraform: simulated failure" >&2
exit %d
`, code)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "terraform"), []byte(script), 0755))
	return dir
}

// 7.1 — forwards args and propagates exit code. Uses unproxied subcommands so
// no emulator/Docker is required.
func TestTerraformForwardsArgs(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ARGS:version")
}

func TestTerraformAliasTF(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "tf", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ARGS:version")
}

// LSTK_TF_CMD selects the binary to invoke (e.g. OpenTofu). A stub named `tofu`
// on PATH must be used instead of `terraform`.
func TestTerraformHonorsLstkTfCmd(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("fake tofu script not supported on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tofu"),
		[]byte("#!/bin/sh\necho \"TOFU:$*\"\n"), 0755))
	e := env.With(env.DisableEvents, "1").With("PATH", dir).With(env.Home, t.TempDir()).
		With(env.Key("LSTK_TF_CMD"), "tofu")

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "version")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "TOFU:version")
}

func TestTerraformPropagatesExitCode(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraformExit(t, 5)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "validate")
	require.Error(t, err)
	assert.Contains(t, stderr, "simulated failure")
	requireExitCode(t, 5, err)
}

func TestTerraformMissingBinary(t *testing.T) {
	t.Parallel()
	// Empty PATH dir → no terraform binary found.
	e := env.With(env.DisableEvents, "1").With("PATH", t.TempDir()).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "version")
	require.Error(t, err)
	combined := stderr + stdout
	assert.Contains(t, combined, "not found in PATH")
	assert.Contains(t, combined, "Install Terraform CLI:")
	assert.Contains(t, combined, "https://developer.hashicorp.com/terraform/cli")
}

// 7.3 — fmt/validate/version/init run without generating an override and without
// a running emulator, even when --region/--account are present (stripped).
func TestTerraformUnproxiedSkipsOverride(t *testing.T) {
	t.Parallel()
	for _, sub := range []string{"fmt", "validate", "version", "init"} {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeTerraform(t)
			workDir := t.TempDir()
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

			stdout, stderr, err := runLstk(t, testContext(t), workDir, e,
				"terraform", "--region", "us-west-2", "--account", "111111111111", sub)
			require.NoError(t, err, "stderr: %s", stderr)

			assert.Contains(t, stdout, "ARGS:"+sub)
			// lstk-specific flags must not be forwarded to terraform.
			assert.NotContains(t, stdout, "--region")
			assert.NotContains(t, stdout, "--account")
			// No override file generated.
			assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
		})
	}
}

// DEVX-1002 — --help (and -h) never require the emulator/provider schema, even
// in an uninitialized project with no running emulator, and are forwarded to
// terraform untouched.
func TestTerraformHelpSkipsOverride(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"-help"}, {"plan", "--help"}} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			fakeDir := writeFakeTerraform(t)
			workDir := t.TempDir()
			e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

			cmdArgs := append([]string{"terraform"}, args...)
			stdout, stderr, err := runLstk(t, testContext(t), workDir, e, cmdArgs...)
			require.NoError(t, err, "stderr: %s", stderr)

			assert.Contains(t, stdout, "ARGS:"+strings.Join(args, " "))
			assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
		})
	}
}

// 7.6 (validation) — invalid --account fails before terraform is invoked. No
// emulator needed because validation happens at the command boundary.
func TestTerraformInvalidAccountRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"terraform", "--account", "12345", "plan")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "12-digit")
}

func TestTerraformMissingFlagValue(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "--region")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "--region requires a value")
}

// 7.7 — positional rules. Flags after the action are forwarded verbatim (use an
// unproxied subcommand to avoid needing an emulator); a flag before the
// subcommand is rejected by Cobra as an unknown root flag.
func TestTerraformFlagsAfterActionAreForwarded(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"terraform", "version", "--region", "us-west-2")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Contains(t, stdout, "ARGS:version --region us-west-2")
}

func TestTerraformFlagBeforeSubcommandRejected(t *testing.T) {
	t.Parallel()
	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), e,
		"--account", "111111111111", "terraform", "version")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "must appear after the terraform subcommand")
}

// 7.2 — proxied command with no running emulator fails with "not running" and
// does not invoke terraform.
func TestTerraformFailsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With(env.LocalStackHost, deadLocalStackHost)

	stdout, _, err := runLstk(t, testContext(t), t.TempDir(), e, "terraform", "plan")
	require.Error(t, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack:")
	assert.NotContains(t, stdout, "ARGS:plan")
}

// 10.4 — lstk terraform only works with the AWS emulator. When a non-AWS
// emulator is running (and AWS is not), it fails with an AWS-specific error
// that names the running emulator, and does not invoke terraform.
func TestTerraformRequiresAWSEmulator(t *testing.T) {
	requireDocker(t)
	cleanup()
	cleanupSnowflake()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupSnowflake)

	ctx := testContext(t)
	startTestSnowflakeContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, ctx, t.TempDir(), e, "terraform", "plan")
	require.Error(t, err)
	assert.Contains(t, stdout, "requires the")
	assert.Contains(t, stdout, "Snowflake")
	assert.NotContains(t, stdout, "ARGS:plan")
}

// 7.4 + 7.6 (encoding) — LSTK_TF_DRY_RUN generates the override (with resolved
// region/account encoded) and skips terraform.
func TestTerraformDryRunGeneratesOverride(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
		With(env.Key("LSTK_TF_DRY_RUN"), "1")

	stdout, stderr, err := runLstk(t, ctx, workDir, e,
		"terraform", "--region", "us-west-2", "--account", "111111111111", "plan")
	require.NoError(t, err, "stderr: %s", stderr)

	// terraform plan must NOT have run.
	assert.NotContains(t, stdout, "ARGS:plan")

	overridePath := filepath.Join(workDir, tfOverrideFile)
	require.FileExists(t, overridePath)
	content, err := os.ReadFile(overridePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), `region = "us-west-2"`)
	assert.Contains(t, string(content), `access_key = "111111111111"`)
	assert.Contains(t, string(content), "endpoints {")
	assert.Contains(t, string(content), "s3 =")
}

// 7.5 — a pre-existing override file (not created by lstk) causes a clear
// failure and is not deleted.
func TestTerraformRefusesPreexistingOverride(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	workDir := t.TempDir()
	overridePath := filepath.Join(workDir, tfOverrideFile)
	require.NoError(t, os.WriteFile(overridePath, []byte("# my own override\n"), 0644))

	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "plan")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "refusing to overwrite")

	content, err := os.ReadFile(overridePath)
	require.NoError(t, err)
	assert.Equal(t, "# my own override\n", string(content), "user's file must be untouched")
}

// 7.8 — a proxied `plan` generates the override (with endpoint keys sourced from
// the provider schema) and removes it after terraform exits. A fully real
// terraform-against-real-LocalStack run isn't possible here (the integration
// harness uses an alpine stand-in for the emulator), so the stub echoes the
// override it sees so we can assert it existed with the schema-derived s3 key.
func TestTerraformGeneratesAndRemovesOverride(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "plan")
	require.NoError(t, err, "stderr: %s", stderr)

	// terraform ran and saw an override carrying the schema-derived s3 endpoint key.
	assert.Contains(t, stdout, "ARGS:plan")
	assert.Contains(t, stdout, "TF>")
	assert.Contains(t, stdout, "s3 =")
	// The override is removed once the run completes.
	assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
}

// 11.4 — a proxied command run with `-chdir=DIR` anchors lstk's work to DIR.
// A dry run proves the override is generated inside DIR (not the process working
// dir) with schema-derived keys; a live run proves `-chdir=DIR` is forwarded to
// terraform and the override is cleaned up afterward.
func TestTerraformChdirAnchorsOverrideToDir(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)
	fakeDir := writeFakeTerraform(t)

	// Dry run leaves the override in place, so we can read it directly from the
	// chdir dir to assert both its location and its schema-derived contents.
	t.Run("override generated inside chdir dir", func(t *testing.T) {
		workDir := t.TempDir()
		infra := filepath.Join(workDir, "infra")
		require.NoError(t, os.Mkdir(infra, 0755))
		e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir()).
			With(env.Key("LSTK_TF_DRY_RUN"), "1")

		_, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "-chdir=infra", "plan")
		require.NoError(t, err, "stderr: %s", stderr)

		overridePath := filepath.Join(infra, tfOverrideFile)
		require.FileExists(t, overridePath)
		content, err := os.ReadFile(overridePath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "s3 =") // schema-derived endpoint key
		// Nothing was written at the top level.
		assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
	})

	// A live run forwards -chdir to terraform and removes the override after.
	t.Run("chdir forwarded and override cleaned up", func(t *testing.T) {
		workDir := t.TempDir()
		infra := filepath.Join(workDir, "infra")
		require.NoError(t, os.Mkdir(infra, 0755))
		e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

		stdout, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "-chdir=infra", "plan")
		require.NoError(t, err, "stderr: %s", stderr)

		assert.Contains(t, stdout, "ARGS:-chdir=infra plan")
		assert.NoFileExists(t, filepath.Join(infra, tfOverrideFile))
		assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
	})
}

// 11.5 — a `-chdir=DIR` pointing at a nonexistent directory fails with a clear
// error before terraform is invoked or an override is generated.
func TestTerraformChdirMissingDirFails(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraform(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, _, err := runLstk(t, ctx, workDir, e, "terraform", "-chdir=does-not-exist", "plan")
	require.Error(t, err)
	assert.Contains(t, stdout, "does not exist")
	assert.NotContains(t, stdout, "ARGS:")
	assert.NoFileExists(t, filepath.Join(workDir, "does-not-exist", tfOverrideFile))
}

// 7.9 — provider schema unavailable (before init) fails with the init-required
// message and neither invokes terraform nor generates an override.
func TestTerraformSchemaUnavailableRequiresInit(t *testing.T) {
	requireDocker(t)
	cleanup()
	t.Cleanup(cleanup)
	ctx := testContext(t)
	startTestContainer(t, ctx)

	fakeDir := writeFakeTerraformFailingSchema(t)
	workDir := t.TempDir()
	e := env.With(env.DisableEvents, "1").With("PATH", fakeDir).With(env.Home, t.TempDir())

	stdout, stderr, err := runLstk(t, ctx, workDir, e, "terraform", "plan")
	require.Error(t, err)
	assert.Contains(t, stderr+stdout, "terraform init")
	assert.NotContains(t, stdout, "ARGS:plan")
	assert.NoFileExists(t, filepath.Join(workDir, tfOverrideFile))
}
