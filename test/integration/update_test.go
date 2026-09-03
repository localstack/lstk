package integration_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/snap"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateCheckCommand(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	ctx := testContext(t)

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "update", "--check", "--non-interactive")
	require.NoError(t, err, "lstk update --check --non-interactive failed: %s", stderr)
	requireExitCode(t, 0, err)
	assert.Contains(t, stdout, "Note:", "should show a note in non-interactive mode")
}

func TestUpdateCheckCommandJSON(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	stdout, stderr, err := runLstk(t, ctx, "", testEnvWithHome(t.TempDir(), ""), "update", "--check", "--json")
	require.NoError(t, err, "lstk update --check --json failed: %s", stderr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	assert.Equal(t, "update", envelope.Command)

	var data struct {
		CurrentVersion  string `json:"currentVersion"`
		LatestVersion   string `json:"latestVersion"`
		UpdateAvailable bool   `json:"updateAvailable"`
	}
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	// The integration test binary is a dev build, so Check short-circuits
	// before any network call — this exercises the "checked, none applied"
	// UpdateCheckedEvent shape without depending on network access.
	assert.Equal(t, "dev", data.CurrentVersion)
	assert.False(t, data.UpdateAvailable)
}

func requireNPM(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not available")
	}
}

func TestUpdateNPMInstall(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

	// Run update — should download the real latest release from GitHub and
	// replace itself. HOME is a temp dir so config/log side effects stay
	// isolated; the binary itself lives in a temp dir already.
	updateCmd := exec.CommandContext(ctx, tmpBinary, "update", "--non-interactive")
	updateCmd.Env = testEnvWithHome(t.TempDir(), "")
	updateOut, err := updateCmd.CombinedOutput()
	updateStr := string(updateOut)
	require.NoError(t, err, "lstk update failed: %s", updateStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, updateStr, "Update available: 0.0.1", "should detect update")
	assert.Contains(t, updateStr, "Downloading and verifying update", "should download and verify binary")
	assert.Contains(t, updateStr, "Updated to", "should complete the update")

	// Verify the binary was replaced with the release version the update
	// reported, not just "anything other than 0.0.1".
	m := regexp.MustCompile(`Updated to v?([0-9]+\.[0-9]+\.[0-9]+\S*)`).FindStringSubmatch(updateStr)
	require.Len(t, m, 2, "update output should report the installed version: %s", updateStr)
	verCmd2 := exec.CommandContext(ctx, tmpBinary, "--version")
	verOut2, err := verCmd2.CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(verOut2), m[1], "replaced binary should print the downloaded release version")
	assert.NotContains(t, string(verOut2), "0.0.1", "binary should no longer be the old version")
}

// TestUpdateBinaryInPlaceJSON exercises an actual applied update (not just
// --check) under --json: Check's UpdateCheckedEvent always fires now, even on
// the apply path, so this specifically proves EnvelopeSink's UpdateAppliedEvent
// case clears the stale latestVersion/updateAvailable keys rather than
// leaking them into the applied-update data shape.
func TestUpdateBinaryInPlaceJSON(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

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

	updateCmd := exec.CommandContext(ctx, tmpBinary, "update", "--non-interactive", "--json")
	updateCmd.Env = testEnvWithHome(t.TempDir(), "")
	updateOut, err := updateCmd.CombinedOutput()
	updateStr := string(updateOut)
	require.NoError(t, err, "lstk update --json failed: %s", updateStr)
	requireExitCode(t, 0, err)

	envelope := decodeEnvelope(t, strings.TrimSpace(updateStr))
	assert.Equal(t, "ok", envelope.Status)

	var data map[string]any
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	assert.Equal(t, "0.0.1", data["currentVersion"])
	assert.Equal(t, true, data["updated"])
	assert.Equal(t, "binary", data["method"])
	assert.NotEmpty(t, data["updatedVersion"])
	_, hasLatestVersion := data["latestVersion"]
	_, hasUpdateAvailable := data["updateAvailable"]
	assert.False(t, hasLatestVersion, "applied-update data should not carry a stale latestVersion key from the preceding check")
	assert.False(t, hasUpdateAvailable, "applied-update data should not carry a stale updateAvailable key from the preceding check")
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
	t.Parallel()

	ctx := testContext(t)

	// Build a fake old version to a temp location
	tmpBinary := filepath.Join(t.TempDir(), execName("lstk"))
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
	t.Cleanup(mockServer.Close)

	t.Run("skip", func(t *testing.T) {
		t.Parallel()
		configFile := filepath.Join(t.TempDir(), "config.toml")
		originalConfig := `# User-maintained lstk config
[[containers]]
type = "aws"     # Emulator type
tag  = "latest"  # Docker image tag
port = "4566"    # Host port
`
		require.NoError(t, os.WriteFile(configFile, []byte(originalConfig), 0o644))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, tmpBinary, "--config", configFile)
		cmd.Env = env.Without(env.AuthToken).With(env.AuthToken, "fake-token").With(env.APIEndpoint, mockServer.URL)

		p := startCmdInPTY(t, ctx, cmd)
		p.waitForOutput("New lstk version available", "update notification prompt should appear")
		p.write("s")

		out, _ := p.wait()
		assert.Contains(t, out, "New lstk version available")

		configData, err := os.ReadFile(configFile)
		require.NoError(t, err)
		configStr := string(configData)
		assert.Contains(t, configStr, "update_skipped_version", "skipped version should be persisted")
		assert.Contains(t, configStr, "# User-maintained lstk config", "file header comment should be preserved")
		assert.Contains(t, configStr, "# Emulator type", "inline comments should be preserved")
		assert.Contains(t, configStr, `port = "4566"`, "existing config values should be preserved")
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()
		// Copy binary since it will be replaced during the update
		updateBinary := filepath.Join(t.TempDir(), execName("lstk"))
		data, err := os.ReadFile(tmpBinary)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(updateBinary, data, 0o755))

		configFile := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configFile, []byte(""), 0o644))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, updateBinary, "--config", configFile)
		cmd.Env = env.Without(env.AuthToken).With(env.AuthToken, "fake-token").With(env.APIEndpoint, mockServer.URL)

		p := startCmdInPTY(t, ctx, cmd)
		p.waitForOutput("New lstk version available", "update notification prompt should appear")
		p.write("u")

		out, err := p.wait()
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

// buildLstkWithVersion builds the lstk binary from the repo root with the
// given version stamped in, writing it to outPath.
func buildLstkWithVersion(t *testing.T, ctx context.Context, version, outPath string) {
	t.Helper()
	buildLstkWithBundledSet(t, ctx, version, "", outPath)
}

// buildLstkWithBundledSet builds lstk with both release stamps the updater
// reads: the version, and the bundled set this release is expected to ship
// (empty for a release that ships none, which is every release today).
func buildLstkWithBundledSet(t *testing.T, ctx context.Context, version, bundledSet, outPath string) {
	t.Helper()
	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	ldflags := "-X github.com/localstack/lstk/internal/version.version=" + version
	if bundledSet != "" {
		ldflags += " -X github.com/localstack/lstk/internal/version.bundledSet=" + bundledSet
	}
	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-ldflags", ldflags,
		"-o", outPath,
		".",
	)
	buildCmd.Dir = repoRoot
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(out))
}

// releaseAssetName mirrors the goreleaser asset naming the updater expects:
// lstk_<version>_<goos>_<goarch>.tar.gz (zip on Windows).
func releaseAssetName(ver string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("lstk_%s_%s_%s.%s", ver, runtime.GOOS, runtime.GOARCH, ext)
}

// releaseMember is one file at the root of a release archive.
type releaseMember struct {
	name string
	body []byte
	mode os.FileMode
}

// packageReleaseArchive wraps binary bytes into the release archive format the
// updater extracts: a tar.gz (zip on Windows) with a single executable entry.
func packageReleaseArchive(t *testing.T, binaryName string, binary []byte) []byte {
	t.Helper()
	return packageReleaseArchiveWith(t, []releaseMember{{name: binaryName, body: binary, mode: 0o755}})
}

// packageReleaseArchiveWith builds a release archive carrying the given members
// at its root, so a test can express a case as "an archive containing X".
func packageReleaseArchiveWith(t *testing.T, members []releaseMember) []byte {
	t.Helper()
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for _, m := range members {
			hdr := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
			hdr.SetMode(m.mode)
			w, err := zw.CreateHeader(hdr)
			require.NoError(t, err)
			_, err = w.Write(m.body)
			require.NoError(t, err)
		}
		require.NoError(t, zw.Close())
		return buf.Bytes()
	}
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, m := range members {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     m.name,
			Mode:     int64(m.mode),
			Size:     int64(len(m.body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write(m.body)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// mockGitHubReleaseServer serves the two GitHub endpoints the updater talks
// to on one host: the releases/latest API (api.github.com in production) and
// the release asset downloads (github.com). Tests point the binary at it via
// mockGitHubEnv.
func mockGitHubReleaseServer(t *testing.T, tag string, assets map[string][]byte) *httptest.Server {
	t.Helper()
	downloadPrefix := "/localstack/lstk/releases/download/" + tag + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/localstack/lstk/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
		case strings.HasPrefix(r.URL.Path, downloadPrefix):
			body, ok := assets[strings.TrimPrefix(r.URL.Path, downloadPrefix)]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// mockGitHubEnv builds an isolated test environment whose updater GitHub
// endpoints (release-metadata API and asset downloads) both point at the
// given mock server.
func mockGitHubEnv(t *testing.T, srv *httptest.Server) []string {
	t.Helper()
	return append(testEnvWithHome(t.TempDir(), ""),
		string(env.UpdateGitHubAPIEndpoint)+"="+srv.URL,
		string(env.UpdateGitHubDownloadEndpoint)+"="+srv.URL,
	)
}

// TestUpdateBinaryMockGitHubHappyPath exercises the full binary update flow
// against a mock GitHub: version check, checksums.txt fetch, archive download,
// SHA-256 verification, and in-place replacement — without touching the
// network or depending on a real release.
func TestUpdateBinaryMockGitHubHappyPath(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	oldBinary := filepath.Join(t.TempDir(), binaryName)
	buildLstkWithVersion(t, ctx, "0.0.1", oldBinary)

	newBinary := filepath.Join(t.TempDir(), binaryName)
	buildLstkWithVersion(t, ctx, "0.0.2", newBinary)
	newBytes, err := os.ReadFile(newBinary)
	require.NoError(t, err)

	archive := packageReleaseArchive(t, binaryName, newBytes)
	sum := sha256.Sum256(archive)
	assetName := releaseAssetName("0.0.2")
	manifest := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)

	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{
		"checksums.txt": []byte(manifest),
		assetName:       archive,
	})

	updateCmd := exec.CommandContext(ctx, oldBinary, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.NoError(t, err, "lstk update failed: %s", outStr)
	requireExitCode(t, 0, err)
	// The snapshot pins the full check → download/verify → updated sequence.
	snap.Match(t, sanitizeOutput(outStr))

	verOut, err := exec.CommandContext(ctx, oldBinary, "--version").CombinedOutput()
	require.NoError(t, err)
	snap.Match(t, sanitizeOutput(string(verOut)))

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(oldBinary), "lstk-update-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "download temp files should be cleaned up")
}

// TestUpdateBinaryMockGitHubChecksumMismatch proves the checksum gate: when
// the downloaded archive does not match checksums.txt, the update aborts with
// a non-zero exit and the installed binary is left untouched. The mock inputs
// are fully deterministic, so stdout and stderr are asserted byte-for-byte.
func TestUpdateBinaryMockGitHubChecksumMismatch(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	oldBinary := filepath.Join(t.TempDir(), binaryName)
	buildLstkWithVersion(t, ctx, "0.0.1", oldBinary)

	// The archive never gets extracted, so garbage bytes suffice; the manifest
	// digest is for different content, so verification must fail.
	archive := []byte("tampered archive bytes")
	archiveSum := sha256.Sum256(archive)
	wrongSum := sha256.Sum256([]byte("what the archive should have been"))
	assetName := releaseAssetName("0.0.2")
	manifest := fmt.Sprintf("%s  %s\n", hex.EncodeToString(wrongSum[:]), assetName)

	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{
		"checksums.txt": []byte(manifest),
		assetName:       archive,
	})

	updateCmd := exec.CommandContext(ctx, oldBinary, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	var stdout, stderr bytes.Buffer
	updateCmd.Stdout = &stdout
	updateCmd.Stderr = &stderr
	err := updateCmd.Run()
	require.Error(t, err, "update must fail on checksum mismatch, output: %s%s", stdout.String(), stderr.String())
	requireExitCode(t, 1, err)

	wantStdout := "Checking for updates...\n" +
		"Update available: 0.0.1 → v0.0.2\n" +
		"Downloading and verifying update...\n" +
		fmt.Sprintf("Error: update failed: checksum mismatch for %s: expected %s, got %s — the downloaded archive may be corrupted or tampered with; update aborted\n",
			assetName, hex.EncodeToString(wrongSum[:]), hex.EncodeToString(archiveSum[:]))
	assert.Equal(t, wantStdout, stdout.String(), "full stdout should be exactly the check/download/error sequence")
	assert.Empty(t, stderr.String(), "silent-error handling must not print anything to stderr")

	verOut, err := exec.CommandContext(ctx, oldBinary, "--version").CombinedOutput()
	require.NoError(t, err, "original binary should still run")
	snap.Match(t, sanitizeOutput(string(verOut)))

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(oldBinary), "lstk-update-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "rejected download must not leave temp files behind")
}

// TestUpdateBinaryMockGitHubMissingChecksums proves the fail-closed contract:
// a release without a checksums.txt asset must abort the update (never fall
// back to installing unverified bytes) and leave the installed binary alone.
func TestUpdateBinaryMockGitHubMissingChecksums(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	oldBinary := filepath.Join(t.TempDir(), binaryName)
	buildLstkWithVersion(t, ctx, "0.0.1", oldBinary)

	// The release asset exists and would extract fine — only the manifest is
	// missing. The updater must refuse before ever downloading the archive.
	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{
		releaseAssetName("0.0.2"): []byte("plausible archive bytes"),
	})

	updateCmd := exec.CommandContext(ctx, oldBinary, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.Error(t, err, "update must fail when checksums.txt is missing, output: %s", outStr)
	requireExitCode(t, 1, err)
	assert.Contains(t, outStr, "no checksums.txt asset", "should name the missing manifest")
	assert.Contains(t, outStr, "refusing to install an unverifiable binary", "should state the fail-closed policy")

	verOut, err := exec.CommandContext(ctx, oldBinary, "--version").CombinedOutput()
	require.NoError(t, err, "original binary should still run")
	snap.Match(t, sanitizeOutput(string(verOut)))

	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(oldBinary), "lstk-update-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "aborted update must not leave temp files behind")
}

// bundledSetMembers is the archive-root names of the set a bundling release
// ships on this platform: the multi-call extensions binary and the descriptions
// file. It is what the release stamps into the binary via ldflags.
func bundledSetMembers() (bundledBinary, descriptions string) {
	bundledBinary = "bundled-extensions"
	if runtime.GOOS == "windows" {
		bundledBinary += ".exe"
	}
	return bundledBinary, "lstk-extensions.toml"
}

// TestUpdateRepairsIncompleteBundledSet is the transition case end to end: a
// user whose old updater installed only the lstk binary is left current but
// without bundled extensions, and cannot wait for a newer release to get them.
// `lstk update` must notice the incomplete set, reinstall the same version, and
// land the missing members.
func TestUpdateRepairsIncompleteBundledSet(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	bundledBinary, descriptions := bundledSetMembers()

	// The installed binary is already the latest version, and was built by the
	// bundling release, but its install directory holds nothing else, exactly
	// as a pre-bundling updater would have left it.
	installDir := t.TempDir()
	installed := filepath.Join(installDir, binaryName)
	buildLstkWithBundledSet(t, ctx, "0.0.2", bundledBinary+","+descriptions, installed)

	newBytes, err := os.ReadFile(installed)
	require.NoError(t, err)
	archive := packageReleaseArchiveWith(t, []releaseMember{
		{name: binaryName, body: newBytes, mode: 0o755},
		{name: bundledBinary, body: []byte("multi-call extensions binary"), mode: 0o755},
		{name: descriptions, body: []byte("deploy = \"Deploy your application\"\n"), mode: 0o644},
	})
	sum := sha256.Sum256(archive)
	assetName := releaseAssetName("0.0.2")
	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)),
		assetName:       archive,
	})

	updateCmd := exec.CommandContext(ctx, installed, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.NoError(t, err, "lstk update failed: %s", outStr)
	requireExitCode(t, 0, err)

	assert.NotContains(t, outStr, "Already up to date",
		"a current binary with an incomplete set must not short-circuit: %s", outStr)
	assert.Contains(t, outStr, "Bundled extensions are missing", "should state the finding")
	assert.Contains(t, outStr, "Reinstalling 0.0.2 to restore bundled extensions",
		"the apply path should narrate the reinstall: %s", outStr)

	// The missing members are now installed where lstk resolves them.
	body, err := os.ReadFile(filepath.Join(installDir, bundledBinary))
	require.NoError(t, err, "the multi-call binary should have been installed")
	assert.Equal(t, "multi-call extensions binary", string(body))
	body, err = os.ReadFile(filepath.Join(installDir, descriptions))
	require.NoError(t, err, "the descriptions file should have been installed")
	assert.Equal(t, "deploy = \"Deploy your application\"\n", string(body))

	leftovers, err := filepath.Glob(filepath.Join(installDir, "*.lstk-new"))
	require.NoError(t, err)
	assert.Empty(t, leftovers, "a completed update leaves no staging files")
}

// TestUpdateReportsUpToDateWhenBundledSetComplete is the other half of the
// repair gate: with the version unchanged and the set complete, the ordinary
// up-to-date path must stay exactly as cheap as it was. The mock release serves
// no checksums.txt and no archive, so any attempt to download would fail the
// update loudly rather than pass unnoticed.
func TestUpdateReportsUpToDateWhenBundledSetComplete(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	bundledBinary, descriptions := bundledSetMembers()

	installDir := t.TempDir()
	installed := filepath.Join(installDir, binaryName)
	buildLstkWithBundledSet(t, ctx, "0.0.2", bundledBinary+","+descriptions, installed)
	require.NoError(t, os.WriteFile(filepath.Join(installDir, bundledBinary), []byte("multi-call"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(installDir, descriptions), []byte("deploy = \"Deploy\"\n"), 0o644))

	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{})

	updateCmd := exec.CommandContext(ctx, installed, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.NoError(t, err, "lstk update should succeed without downloading anything: %s", outStr)
	requireExitCode(t, 0, err)
	assert.Contains(t, outStr, "Already up to date", "output was: %s", outStr)
	assert.NotContains(t, outStr, "Downloading", "a complete set must not trigger a download: %s", outStr)
}

// TestUpdateIgnoresBundledSetOnPreBundlingRelease pins the rollback and
// pre-bundling behavior: a binary that ships no bundle never probes the install
// directory, so its up-to-date path is unchanged even with nothing beside it.
func TestUpdateIgnoresBundledSetOnPreBundlingRelease(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	installDir := t.TempDir()
	installed := filepath.Join(installDir, binaryName)
	buildLstkWithVersion(t, ctx, "0.0.2", installed)

	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{})

	updateCmd := exec.CommandContext(ctx, installed, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.NoError(t, err, "lstk update failed: %s", outStr)
	assert.Contains(t, outStr, "Already up to date", "output was: %s", outStr)
	assert.NotContains(t, outStr, "Bundled extensions are missing", "output was: %s", outStr)
}

// TestUpdateRepairFailsLoudlyWhenArchiveLacksMembers guards against the silent
// repair loop: the binary is stamped with a bundled set, the release archive
// for the same version does not carry it, and without a post-install check
// every `lstk update` would re-download, report success, and fix nothing,
// forever. The update must fail and name what the archive did not deliver.
func TestUpdateRepairFailsLoudlyWhenArchiveLacksMembers(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	bundledBinary, descriptions := bundledSetMembers()

	installDir := t.TempDir()
	installed := filepath.Join(installDir, binaryName)
	buildLstkWithBundledSet(t, ctx, "0.0.2", bundledBinary+","+descriptions, installed)

	// The archive carries only lstk: the stamp promises members the release
	// does not deliver.
	newBytes, err := os.ReadFile(installed)
	require.NoError(t, err)
	archive := packageReleaseArchive(t, binaryName, newBytes)
	sum := sha256.Sum256(archive)
	assetName := releaseAssetName("0.0.2")
	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)),
		assetName:       archive,
	})

	updateCmd := exec.CommandContext(ctx, installed, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	outStr := string(out)
	require.Error(t, err, "a repair that restored nothing must not report success: %s", outStr)
	requireExitCode(t, 1, err)
	assert.Contains(t, outStr, "did not restore", "should say the repair failed: %s", outStr)
	assert.Contains(t, outStr, bundledBinary, "should name the member the archive lacks: %s", outStr)
	assert.NotContains(t, outStr, "Updated to", "must not claim success: %s", outStr)
}

// TestUpdateUpToDateCleansStagingLeftovers: a repair interrupted between
// committing the members and committing lstk itself leaves a staging copy of
// lstk behind, and the next run reports up to date without ever staging. The
// up-to-date path must still tidy those leftovers instead of leaking a
// binary-sized file until the next release.
func TestUpdateUpToDateCleansStagingLeftovers(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binaryName := "lstk"
	if runtime.GOOS == "windows" {
		binaryName = "lstk.exe"
	}
	installDir := t.TempDir()
	installed := filepath.Join(installDir, binaryName)
	buildLstkWithVersion(t, ctx, "0.0.2", installed)
	leftover := filepath.Join(installDir, binaryName+".lstk-new")
	require.NoError(t, os.WriteFile(leftover, []byte("interrupted staging copy"), 0o755))

	srv := mockGitHubReleaseServer(t, "v0.0.2", map[string][]byte{})

	updateCmd := exec.CommandContext(ctx, installed, "update", "--non-interactive")
	updateCmd.Env = mockGitHubEnv(t, srv)
	out, err := updateCmd.CombinedOutput()
	require.NoError(t, err, "lstk update failed: %s", string(out))
	assert.Contains(t, string(out), "Already up to date")

	_, statErr := os.Stat(leftover)
	assert.True(t, os.IsNotExist(statErr), "the up-to-date path should clean staging leftovers")
}
