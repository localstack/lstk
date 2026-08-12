package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/must"
	"github.com/localstack/lstk/test/integration/env"
)

// runAzRaw runs `az <args>` directly (not via lstk) with the given environment. The
// interception commands mutate the *global* Azure CLI config (no AZURE_CONFIG_DIR), so
// to verify their effect we must read it back with a plain `az` invocation that uses the
// same isolated HOME — `lstk az ...` would inject the isolated config dir instead.
func runAzRaw(t *testing.T, ctx context.Context, environ []string, args ...string) (string, error) {
	t.Helper()
	azBin, err := exec.LookPath("az")
	must.NoError(t, err)
	cmd := exec.CommandContext(ctx, azBin, args...)
	cmd.Env = environ
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err = cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// Routing: start-interception is a subcommand of `az`, so it must NOT fall through to the
// passthrough handler (which would report "not set up" because there is no setup marker).
// With `az` absent it should report the subcommand's own "az CLI not found" instead.
func TestAzStartInterceptionRoutesToSubcommand(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t) // deliberately no setup marker

	stdout, _, err := runLstk(t, testContext(t), workDir,
		env.WithHome(t.TempDir()).With("PATH", t.TempDir()),
		"az", "start-interception",
	)
	must.Error(t, err)
	must.Contains(t, stdout, "az CLI not found in PATH")
	must.NotContains(t, stdout, "not set up")
}

// stop-interception is a safe no-op when LocalStack is not the active cloud — e.g.
// interception was never started, so the global ~/.azure still points at real Azure.
// It must report the current cloud and exit 0 without changing anything (and without
// needing the emulator running). Requires only the Azure CLI, not Docker.
func TestAzStopInterceptionNoOpWhenNotIntercepting(t *testing.T) {
	requireAzCLI(t)
	t.Parallel()
	workDir := azureWorkDir(t)
	home := azTempHome(t) // fresh ~/.azure: active cloud is the default AzureCloud

	stdout, _, err := runLstk(t, testContext(t), workDir,
		env.WithHome(home),
		"az", "stop-interception",
	)
	must.NoError(t, err, "stop-interception must not fail when LocalStack is not active")
	must.Contains(t, stdout, "not the active Azure cloud")

	// It must not have switched the active cloud to anything.
	active, azErr := runAzRaw(t, testContext(t), env.WithHome(home), "cloud", "show", "--query", "name", "-o", "tsv")
	must.NoError(t, azErr)
	must.Eq(t, "AzureCloud", active)
}

func TestAzStopInterceptionFailsWhenAzureCLINotInstalled(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t)

	stdout, _, err := runLstk(t, testContext(t), workDir,
		env.WithHome(t.TempDir()).With("PATH", t.TempDir()),
		"az", "stop-interception",
	)
	must.Error(t, err)
	must.Contains(t, stdout, "az CLI not found in PATH")
	must.Contains(t, stdout, "Install Azure CLI:")
}

// TestAzInterception exercises the full start/stop-interception lifecycle against a real
// emulator: it requires Docker, the Azure CLI, and LOCALSTACK_AUTH_TOKEN. It reuses one
// running emulator for all scenarios (the default, --cloud override, the not-active no-op
// guard, and unknown-cloud rejection) since emulator tests cannot run in parallel.
func TestAzInterception(t *testing.T) {
	requireDocker(t)
	requireAzCLI(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	tmpHome := t.TempDir()
	// The emulator runs as root and writes root-owned files into the lstk volume dir;
	// Go's TempDir cleanup can't remove those without help.
	t.Cleanup(func() {
		volumeDir := filepath.Join(tmpHome, ".cache", "lstk", "volume")
		if _, err := os.Stat(volumeDir); err == nil {
			_ = exec.Command("docker", "run", "--rm", "-v", volumeDir+":/d", "alpine", "sh", "-c", "rm -rf /d/*").Run()
		}
	})

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	baseEnv := env.With(env.AuthToken, env.Get(env.AuthToken)).WithHome(tmpHome).With(env.APIEndpoint, mockServer.URL)
	workDir := azureWorkDir(t)
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, workDir, baseEnv, "start")
	must.NoError(t, err, "lstk start failed: %s", stderr)

	activeCloud := func() string {
		// baseEnv carries HOME=tmpHome and no AZURE_CONFIG_DIR, so this reads the same
		// global config that interception mutated.
		name, azErr := runAzRaw(t, ctx, baseEnv, "cloud", "show", "--query", "name", "-o", "tsv")
		must.NoError(t, azErr)
		return name
	}

	// start-interception: global `az` now points at LocalStack.
	stdout, stderr, err := runLstk(t, ctx, workDir, baseEnv, "az", "start-interception")
	must.NoError(t, err, "start-interception failed: stdout=%s stderr=%s", stdout, stderr)
	must.Contains(t, stdout, "Interception active")
	must.Contains(t, stdout, "stop-interception")
	must.Eq(t, "LocalStack", activeCloud(), "LocalStack should be the active global cloud after start-interception")

	// stop-interception (default): back to AzureCloud.
	stdout, stderr, err = runLstk(t, ctx, workDir, baseEnv, "az", "stop-interception")
	must.NoError(t, err, "stop-interception failed: stdout=%s stderr=%s", stdout, stderr)
	must.Contains(t, stdout, "Interception stopped")
	must.Eq(t, "AzureCloud", activeCloud())

	// no-op guard: LocalStack is no longer active, so stop must not clobber AzureCloud.
	stdout, _, err = runLstk(t, ctx, workDir, baseEnv, "az", "stop-interception")
	must.NoError(t, err)
	must.Contains(t, stdout, "not the active Azure cloud")
	must.Eq(t, "AzureCloud", activeCloud())

	// unknown --cloud is rejected (only reached because LocalStack is active again).
	_, _, err = runLstk(t, ctx, workDir, baseEnv, "az", "start-interception")
	must.NoError(t, err)
	must.Eq(t, "LocalStack", activeCloud())
	_, stderr, err = runLstk(t, ctx, workDir, baseEnv, "az", "stop-interception", "--cloud", "NotARealCloud")
	must.Error(t, err)
	must.Contains(t, stderr, "unknown Azure cloud 'NotARealCloud'")
	must.Eq(t, "LocalStack", activeCloud(), "rejected --cloud must not change the active cloud")

	// --cloud override switches to the requested registered cloud.
	stdout, stderr, err = runLstk(t, ctx, workDir, baseEnv, "az", "stop-interception", "--cloud", "AzureChinaCloud")
	must.NoError(t, err, "stop-interception --cloud failed: stdout=%s stderr=%s", stdout, stderr)
	must.Eq(t, "AzureChinaCloud", activeCloud())
}
