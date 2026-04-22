package integration_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCheckCommand(t *testing.T) {
	ctx := testContext(t)

	analyticsSrv, events := mockAnalyticsServer(t)
	stdout, stderr, err := runLstk(t, ctx, "", env.With(env.AnalyticsEndpoint, analyticsSrv.URL), "update", "--check")
	require.NoError(t, err, "lstk update --check failed: %s", stderr)
	requireExitCode(t, 0, err)

	// Dev builds report a note about skipping update check
	assert.Contains(t, stdout, "Note:", "should show a note (dev build or up-to-date)")
	assertCommandTelemetry(t, events, "update", 0)
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

func TestUpdateNPMInstall(t *testing.T) {
	requireNPM(t)

	// Skip if lstk is already installed globally (e.g., via Homebrew).
	// npm install -g fails with EEXIST when it tries to create a symlink
	// over an existing binary at the same path.
	if path, err := exec.LookPath("lstk"); err == nil {
		t.Skipf("lstk already installed at %s, would conflict with npm install -g", path)
	}

	ctx := testContext(t)

	// Set up a fake local npm project so we get a binary inside node_modules.
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

	// Run the binary directly (not through npx) so os.Executable() resolves to the node_modules path.
	// The update should always use `npm install -g` regardless of local/global context.
	cmd := exec.CommandContext(ctx, nmBinaryPath, "update", "--non-interactive")
	cmd.Dir = projectDir
	stdout, err := cmd.CombinedOutput()
	stdoutStr := string(stdout)

	require.NoError(t, err, "lstk update failed: %s", stdoutStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdoutStr, "npm install -g", "should always use global install")
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
	verCmd := exec.CommandContext(ctx, tmpBinary, "--version")
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
	verCmd2 := exec.CommandContext(ctx, tmpBinary, "--version")
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
	verCmd := exec.CommandContext(ctx, caskBinary, "--version")
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

func TestUpdateNotification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	ctx := testContext(t)

	// Build a fake old version to a temp location
	tmpBinary := filepath.Join(t.TempDir(), "lstk")
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

	// Mock API server so license validation fails fast after the notification
	mockServer := createMockLicenseServer(false)
	defer mockServer.Close()

	t.Run("skip", func(t *testing.T) {
		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(""), 0o644))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, tmpBinary, "--config", configFile)
		cmd.Env = env.Without(env.AuthToken).With(env.AuthToken, "fake-token").With(env.APIEndpoint, mockServer.URL)

		ptmx, err := pty.Start(cmd)
		require.NoError(t, err, "failed to start command in PTY")
		defer func() { _ = ptmx.Close() }()

		output := &syncBuffer{}
		outputCh := make(chan struct{})
		go func() {
			_, _ = io.Copy(output, ptmx)
			close(outputCh)
		}()

		require.Eventually(t, func() bool {
			return bytes.Contains(output.Bytes(), []byte("New lstk version available"))
		}, 10*time.Second, 100*time.Millisecond, "update notification prompt should appear")

		_, err = ptmx.Write([]byte("s"))
		require.NoError(t, err)

		_ = cmd.Wait()
		<-outputCh

		assert.Contains(t, output.String(), "New lstk version available")
	})


	t.Run("update", func(t *testing.T) {
		// Copy binary since it will be replaced during the update
		updateBinary := filepath.Join(t.TempDir(), "lstk")
		data, err := os.ReadFile(tmpBinary)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(updateBinary, data, 0o755))

		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(""), 0o644))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, updateBinary, "--config", configFile)
		cmd.Env = env.Without(env.AuthToken).With(env.AuthToken, "fake-token").With(env.APIEndpoint, mockServer.URL)

		ptmx, err := pty.Start(cmd)
		require.NoError(t, err, "failed to start command in PTY")
		defer func() { _ = ptmx.Close() }()

		output := &syncBuffer{}
		outputCh := make(chan struct{})
		go func() {
			_, _ = io.Copy(output, ptmx)
			close(outputCh)
		}()

		require.Eventually(t, func() bool {
			return bytes.Contains(output.Bytes(), []byte("New lstk version available"))
		}, 10*time.Second, 100*time.Millisecond, "update notification prompt should appear")

		_, err = ptmx.Write([]byte("u"))
		require.NoError(t, err)

		err = cmd.Wait()
		<-outputCh

		out := output.String()
		require.NoError(t, err, "update should succeed: %s", out)
		assert.Contains(t, out, "New lstk version available")
		assert.Contains(t, out, "Updated to")

		// Verify the binary was actually replaced
		verCmd := exec.CommandContext(ctx, updateBinary, "--version")
		verOut, err := verCmd.CombinedOutput()
		require.NoError(t, err)
		assert.NotContains(t, string(verOut), "0.0.1", "binary should no longer be the old version")
	})
}

func npmPlatformPackage() string {
	return "lstk_" + runtime.GOOS + "_" + runtime.GOARCH
}
