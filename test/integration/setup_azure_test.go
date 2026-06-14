package integration_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireAzCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("az"); err != nil {
		t.Skip("az CLI not available")
	}
}

// azureWorkDir prepares a fresh workDir with a project-local `.lstk/config.toml`
// containing an Azure container, and returns its path. Tests run `lstk` with
// `cmd.Dir = workDir` so the project-local config search finds this file —
// `lstk az` has `DisableFlagParsing: true`, so a `--config` flag wouldn't reach
// the parent flag set.
func azureWorkDir(t *testing.T) string {
	t.Helper()
	workDir := t.TempDir()
	lstkDir := filepath.Join(workDir, ".lstk")
	require.NoError(t, os.MkdirAll(lstkDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(lstkDir, "config.toml"), []byte(`
[[containers]]
type = "azure"
tag  = "latest"
port = "4566"
`), 0644))
	return workDir
}

func writeAzureSetupMarker(t *testing.T, workDir string) {
	t.Helper()
	dir := filepath.Join(workDir, ".lstk", "azure")
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".lstk-setup-complete"), []byte("ok\n"), 0600))
}

func TestAzCommandErrorsWhenNotSetUp(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t)

	stdout, _, err := runLstk(t, testContext(t), workDir,
		env.With(env.Home, t.TempDir()),
		"az", "group", "list",
	)
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Azure CLI integration is not set up")
	assert.Contains(t, stdout, "lstk setup azure")
}

// Non-interactive mode must not be rejected upfront (CI use case): with no
// Azure emulator in the config, the setup logic itself runs and reports the
// domain error instead of "requires an interactive terminal".
func TestSetupAzureNonInteractiveRunsWithoutTerminal(t *testing.T) {
	t.Parallel()

	_, stderr, err := runLstk(t, testContext(t), "",
		env.With(env.Home, t.TempDir()),
		"setup", "azure",
	)
	require.Error(t, err)
	assert.Contains(t, stderr, "no azure emulator configured")
	assert.NotContains(t, stderr, "interactive terminal")
}

// When the az CLI is missing, the error must be reported exactly once —
// not as a warning and then again as the final error.
func TestSetupAzureReportsMissingAzCLIOnce(t *testing.T) {
	t.Parallel()
	workDir := azureWorkDir(t)

	stdout, stderr, err := runLstk(t, testContext(t), workDir,
		env.With(env.Home, t.TempDir()).With("PATH", t.TempDir()),
		"setup", "azure",
	)
	require.Error(t, err)
	assert.Contains(t, stderr, "az CLI not found in PATH")
	combined := stdout + stderr
	assert.Equal(t, 1, strings.Count(combined, "az CLI not found in PATH"),
		"missing az CLI must be reported exactly once, got:\n%s", combined)
}

func TestAzCommandErrorsWhenEmulatorNotRunning(t *testing.T) {
	requireDocker(t)
	requireAzCLI(t)
	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	workDir := azureWorkDir(t)
	writeAzureSetupMarker(t, workDir)

	stdout, _, err := runLstk(t, testContext(t), workDir,
		env.With(env.Home, t.TempDir()),
		"az", "group", "list",
	)
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "is not running")
	assert.Contains(t, stdout, "Start LocalStack")
}

// TestSetupAzureAndAzCommandSucceed requires Docker, the Azure CLI, and LOCALSTACK_AUTH_TOKEN.
func TestSetupAzureAndAzCommandSucceed(t *testing.T) {
	requireDocker(t)
	requireAzCLI(t)
	_ = env.Require(t, env.AuthToken)
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	cleanup()
	cleanupAzure()
	t.Cleanup(cleanup)
	t.Cleanup(cleanupAzure)

	tmpHome := t.TempDir()
	// The emulator runs as root and writes root-owned files into the lstk
	// volume dir; Go's TempDir cleanup can't remove those without help.
	t.Cleanup(func() {
		volumeDir := filepath.Join(tmpHome, ".cache", "lstk", "volume")
		if _, err := os.Stat(volumeDir); err == nil {
			_ = exec.Command("docker", "run", "--rm", "-v", volumeDir+":/d", "alpine", "sh", "-c", "rm -rf /d/*").Run()
		}
	})

	baseEnv := env.With(env.AuthToken, env.Get(env.AuthToken)).With(env.Home, tmpHome)
	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	workDir := azureWorkDir(t)
	ctx := testContext(t)

	_, stderr, err := runLstk(t, ctx, workDir,
		baseEnv.With(env.APIEndpoint, mockServer.URL),
		"start",
	)
	require.NoError(t, err, "lstk start failed: %s", stderr)

	binPath, err := filepath.Abs(binaryPath())
	require.NoError(t, err)
	cmd := exec.CommandContext(ctx, binPath, "setup", "azure")
	cmd.Dir = workDir
	cmd.Env = baseEnv.With(env.APIEndpoint, mockServer.URL)
	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start setup azure in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()
	require.NoError(t, cmd.Wait(), "setup azure should succeed; output:\n%s", out.String())
	<-outputCh
	assert.Contains(t, out.String(), "Azure CLI integration ready")

	markerPath := filepath.Join(workDir, ".lstk", "azure", ".lstk-setup-complete")
	_, err = os.Stat(markerPath)
	require.NoError(t, err, "marker file should be written on successful setup")

	// `az cloud show` reads the isolated config dir locally, so the assertion
	// doesn't depend on emulator-side behaviour for any specific Azure service.
	stdout, stderr2, err := runLstk(t, ctx, workDir,
		baseEnv.With(env.APIEndpoint, mockServer.URL),
		"az", "cloud", "show", "--name", "LocalStack",
	)
	require.NoError(t, err, "lstk az cloud show failed: %s", stderr2)
	assert.Contains(t, stdout, "azure.localhost.localstack.cloud:4566",
		"registered cloud should expose the LocalStack Azure endpoint")

	// Setup must also work without a terminal (CI use case): runLstk uses
	// pipes, so this exercises the plain-sink path end to end, updating the
	// already-registered cloud.
	stdoutNI, stderrNI, err := runLstk(t, ctx, workDir,
		baseEnv.With(env.APIEndpoint, mockServer.URL),
		"setup", "azure",
	)
	require.NoError(t, err, "non-interactive setup azure failed: stdout=%s stderr=%s", stdoutNI, stderrNI)
	assert.Contains(t, stdoutNI, "Azure CLI integration ready")
}
