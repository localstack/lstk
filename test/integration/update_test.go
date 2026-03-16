package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCheckCommand(t *testing.T) {
	ctx := testContext(t)

	stdout, stderr, err := runLstk(t, ctx, "", nil, "update", "--check")
	require.NoError(t, err, "lstk update --check failed: %s", stderr)
	requireExitCode(t, 0, err)

	// Dev builds report a note about skipping update check
	assert.Contains(t, stdout, "Note:", "should show a note (dev build or up-to-date)")
}

func TestUpdateCheckCommandNonInteractive(t *testing.T) {
	ctx := testContext(t)

	stdout, stderr, err := runLstk(t, ctx, "", nil, "update", "--check", "--non-interactive")
	require.NoError(t, err, "lstk update --check --non-interactive failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Note:", "should show a note in non-interactive mode")
}

func requireNPM(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not available")
	}
}

func TestUpdateNPMLocalInstall(t *testing.T) {
	requireNPM(t)

	ctx := testContext(t)

	// Set up a fake local npm project.
	// On Windows, t.TempDir() may return a short 8.3 path (e.g. RUNNER~1)
	// while the program resolves the long path. EvalSymlinks normalizes both.
	projectDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(
		filepath.Join(projectDir, "package.json"),
		[]byte(`{"name":"test-project","version":"1.0.0","dependencies":{"@localstack/lstk":"*"}}`),
		0o644,
	))

	// Install @localstack/lstk locally so the node_modules structure exists
	npmInstall := exec.CommandContext(ctx, "npm", "install", "@localstack/lstk")
	npmInstall.Dir = projectDir
	out, err := npmInstall.CombinedOutput()
	require.NoError(t, err, "npm install failed: %s", string(out))

	// Build a fake old version binary and replace the one in node_modules
	platformPkg := npmPlatformPackage()
	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	nmBinaryPath := filepath.Join(projectDir, "node_modules", "@localstack", platformPkg, binaryName)

	// Verify the binary exists from npm install
	_, err = os.Stat(nmBinaryPath)
	require.NoError(t, err, "expected binary at %s after npm install", nmBinaryPath)

	// Build our dev binary with a fake old version into that location
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-X github.com/localstack/lstk/internal/version.version=0.0.1",
		"-o", nmBinaryPath,
		".",
	)
	buildCmd.Dir = repoRoot
	out, err = buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(out))

	// Run the binary directly (not through npx) so os.Executable() resolves to the node_modules path
	cmd := exec.CommandContext(ctx, nmBinaryPath, "update", "--non-interactive")
	cmd.Dir = projectDir
	stdout, err := cmd.CombinedOutput()
	stdoutStr := string(stdout)

	require.NoError(t, err, "lstk update failed: %s", stdoutStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdoutStr, "npm (local)", "should detect local npm install")
	assert.Contains(t, stdoutStr, projectDir, "should show the project directory")
	assert.Contains(t, stdoutStr, "Updated to", "should complete the update")
}

func TestUpdateBinaryInPlace(t *testing.T) {
	ctx := testContext(t)

	// Build a fake old version to a temp location
	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	tmpBinary := filepath.Join(t.TempDir(), binaryName)
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-X github.com/localstack/lstk/internal/version.version=0.0.1",
		"-o", tmpBinary,
		".",
	)
	buildCmd.Dir = repoRoot
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(out))

	// Verify it reports the fake version
	verCmd := exec.CommandContext(ctx, tmpBinary, "version")
	verOut, err := verCmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(verOut), "0.0.1")

	// Run update — should download from GitHub and replace itself
	updateCmd := exec.CommandContext(ctx, tmpBinary, "update", "--non-interactive")
	updateOut, err := updateCmd.CombinedOutput()
	updateStr := string(updateOut)
	require.NoError(t, err, "lstk update failed: %s", updateStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, updateStr, "Update available: 0.0.1", "should detect update")
	assert.Contains(t, updateStr, "Downloading update", "should download binary")
	assert.Contains(t, updateStr, "Updated to", "should complete the update")

	// Verify the binary was actually replaced
	verCmd2 := exec.CommandContext(ctx, tmpBinary, "version")
	verOut2, err := verCmd2.CombinedOutput()
	require.NoError(t, err)
	assert.NotContains(t, string(verOut2), "0.0.1", "binary should no longer be the old version")
}

func requireHomebrew(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("brew"); err != nil {
		t.Skip("Homebrew is not available")
	}
}

func homebrewLstkBinaryPath(t *testing.T) string {
	t.Helper()

	// Find the Caskroom binary by resolving the brew symlink
	brewBin, err := exec.Command("brew", "--prefix").Output()
	require.NoError(t, err)
	prefix := strings.TrimSpace(string(brewBin))

	// Look for lstk in the Caskroom
	matches, err := filepath.Glob(filepath.Join(prefix, "Caskroom", "lstk", "*", "lstk"))
	if err != nil || len(matches) == 0 {
		t.Skip("lstk is not installed via Homebrew")
	}
	return matches[0]
}

func TestUpdateHomebrew(t *testing.T) {
	if os.Getenv("LSTK_TEST_HOMEBREW") != "1" {
		t.Skip("Skipping: overwrites real Homebrew binary. Set LSTK_TEST_HOMEBREW=1 to opt in.")
	}
	requireHomebrew(t)
	caskBinary := homebrewLstkBinaryPath(t)

	ctx := testContext(t)
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	// Build a fake old version into the Caskroom location
	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", "-X github.com/localstack/lstk/internal/version.version=0.0.1",
		"-o", caskBinary,
		".",
	)
	buildCmd.Dir = repoRoot
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(out))

	// Verify it reports the fake version
	verCmd := exec.CommandContext(ctx, caskBinary, "version")
	verOut, err := verCmd.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(verOut), "0.0.1")

	// Run update — should detect Homebrew and run brew upgrade
	// Note: brew may consider lstk already up-to-date (its metadata tracks the
	// cask version, not the actual binary content), so "Updated to" may or may
	// not appear. We verify detection and that brew was invoked without error.
	updateCmd := exec.CommandContext(ctx, caskBinary, "update", "--non-interactive")
	updateOut, err := updateCmd.CombinedOutput()
	updateStr := string(updateOut)
	require.NoError(t, err, "lstk update failed: %s", updateStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, updateStr, "Homebrew", "should detect Homebrew install")
	assert.Contains(t, updateStr, "brew upgrade", "should mention brew upgrade")
}

func npmPlatformPackage() string {
	return "lstk_" + runtime.GOOS + "_" + runtime.GOARCH
}
