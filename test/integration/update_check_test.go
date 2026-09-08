package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/localstack/lstk/test/integration/env"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installLstkUnder copies the built lstk binary into a directory laid out the
// way the named tool manager installs it, and returns the copied binary's path.
// Running that copy is what makes the externally-managed refusal observable
// through the CLI, since detection reads the resolved path of the running
// executable.
func installLstkUnder(t *testing.T, layout string) string {
	t.Helper()

	src, err := filepath.Abs(binaryPath())
	require.NoError(t, err)
	data, err := os.ReadFile(src)
	require.NoError(t, err, "run `make build` first")

	dir := filepath.Join(t.TempDir(), filepath.FromSlash(layout))
	require.NoError(t, os.MkdirAll(dir, 0755))

	name := "lstk"
	if runtime.GOOS == "windows" {
		name = "lstk.exe"
	}
	dst := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(dst, data, 0755))
	return dst
}

// resolvedDir reports the directory holding path after symlink evaluation —
// the same resolution DetectInstallMethod applies, and so what lstk prints.
// Windows requires it: t.TempDir() hands back an 8.3 short name (RUNNER~1)
// where lstk prints the long one (runneradmin), which no substring match can
// bridge. On macOS the unresolved form passed only by accident (/var/... is a
// substring of /private/var/...), so this makes that assertion meaningful too.
func resolvedDir(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	require.NoError(t, err)
	return resolved
}

func TestUpdateRefusesOnMiseManagedInstall(t *testing.T) {
	t.Parallel()

	bin := installLstkUnder(t, ".local/share/mise/installs/github-localstack-lstk/latest")
	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update")

	requireExitCode(t, 1, err)
	combined := stdout + stderr
	assert.Contains(t, combined, "managed by mise", "the refusal must name the manager")
	assert.Contains(t, combined, resolvedDir(t, bin), "the refusal must name the resolved install path")
}

func TestUpdateForceBypassesExternalRefusal(t *testing.T) {
	t.Parallel()

	bin := installLstkUnder(t, ".local/share/mise/installs/github-localstack-lstk/latest")
	// Relies on `make build` stamping no version, so Check short-circuits on
	// the "dev" build. If the integration build ever stamped one, --force here
	// would perform a real download and self-replace; use a mock endpoint then.
	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update", "--force")

	require.NoError(t, err, stderr)
	combined := stdout + stderr
	assert.NotContains(t, combined, "managed by mise", "--force must skip the refusal entirely")
}

func TestUpdateJSONReportsExternallyManagedCode(t *testing.T) {
	t.Parallel()

	bin := installLstkUnder(t, "nix/store/9zk1abcdlstk-lstk-0.5.0/bin")
	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update", "--json")

	requireExitCode(t, 1, err)

	var envelope struct {
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout was: %s / stderr: %s", stdout, stderr)
	assert.Equal(t, "error", envelope.Status)
	assert.Equal(t, "UPDATE_EXTERNALLY_MANAGED", envelope.Error.Code)
	assert.Contains(t, envelope.Error.Message, "nix")
}

func TestUpdateProceedsOnOrdinaryInstall(t *testing.T) {
	t.Parallel()

	bin := installLstkUnder(t, "opt/tools/bin")
	// No --check: --check is exempt from the refusal by design, so it would not
	// exercise the guard at all. Safe without a version stamp because `make
	// build` sets none, so Check short-circuits on the "dev" build before any
	// network call.
	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update")

	require.NoError(t, err, stderr)
	assert.NotContains(t, stdout+stderr, "will not update itself")
	assert.NotContains(t, stdout+stderr, "not writable")
}

func writeConfigWithCLI(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n\n[cli]\n" + body + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestInvalidUpdateCheckInConfigIsRejected(t *testing.T) {
	t.Parallel()

	// "quiet" is not a valid mode; it stands in for a plausible-sounding typo.
	configFile := writeConfigWithCLI(t, `update_check = "quiet"`)
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--config", configFile, "volume", "path")

	requireExitCode(t, 1, err)
	combined := stdout + stderr
	assert.Contains(t, combined, "update_check")
	assert.Contains(t, combined, "quiet")
}

func TestValidUpdateCheckInConfigIsAccepted(t *testing.T) {
	t.Parallel()

	configFile := writeConfigWithCLI(t, `update_check = "off"`)
	_, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""),
		"--config", configFile, "volume", "path")

	require.NoError(t, err, stderr)
}

func TestUpdateRefusesWhenInstallDirIsNotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions do not port to Windows")
	}
	t.Parallel()

	bin := installLstkUnder(t, "opt/tools/bin")
	dir := filepath.Dir(bin)
	// Resolve before chmod, while the directory is still fully traversable.
	want := resolvedDir(t, bin)
	require.NoError(t, os.Chmod(dir, 0500))
	// Restore write permission so t.TempDir cleanup can remove the binary.
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update")

	requireExitCode(t, 1, err)
	assert.Contains(t, stdout+stderr, want, "the refusal must name the directory it cannot write to")
}

// buildStampedLstk builds lstk with a real version number into the given
// layout under a temp dir, and returns the binary path. A version is required
// because `make build` stamps none, and `checkQuietlyWithVersion` skips the
// update check entirely for a "dev" build — so an unstamped binary can never
// exercise the notify/off behavior.
func buildStampedLstk(t *testing.T, layout, version string) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)

	dir := filepath.Join(t.TempDir(), filepath.FromSlash(layout))
	require.NoError(t, os.MkdirAll(dir, 0755))
	name := "lstk"
	if runtime.GOOS == "windows" {
		name = "lstk.exe"
	}
	bin := filepath.Join(dir, name)

	build := exec.CommandContext(testContext(t), "go", "build",
		"-ldflags", "-X github.com/localstack/lstk/internal/version.version="+version,
		"-o", bin, ".")
	build.Dir = repoRoot
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build failed: %s", string(out))
	return bin
}

// countingReleaseServer serves the release-metadata endpoint and records how
// many times it was asked, so a test can prove no request was made at all.
func countingReleaseServer(t *testing.T, tag string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// startEnv runs `lstk start` against an unreachable Docker daemon: the update
// notification is emitted before container.Start, so its output is observable
// without a real emulator.
func startEnv(t *testing.T, srv *httptest.Server, extra ...string) []string {
	t.Helper()
	e := append(testEnvWithHome(t.TempDir(), ""),
		string(env.UpdateGitHubAPIEndpoint)+"="+srv.URL,
		string(env.UpdateGitHubDownloadEndpoint)+"="+srv.URL,
		unreachableDockerHost,
	)
	return append(e, extra...)
}

func TestUpdateCheckOffMakesNoRequestAndSaysNothing(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "off"`)

	stdout, stderr, _ := runBinary(t, t.TempDir(), startEnv(t, srv), bin,
		"--config", configFile, "start", "--non-interactive")

	assert.Equal(t, int32(0), hits.Load(), "off must make no request to the release API")
	assert.NotContains(t, stdout+stderr, "Update available")
}

func TestUpdateCheckNotifyEmitsNoteWithoutBlocking(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "notify"`)

	stdout, stderr, _ := runBinary(t, t.TempDir(), startEnv(t, srv), bin,
		"--config", configFile, "start", "--non-interactive")

	assert.Equal(t, int32(1), hits.Load(), "notify must still check")
	assert.Contains(t, stdout+stderr, "Update available: 0.0.1 → v9.9.9")
}

func TestUpdateCheckEnvVarOverridesConfig(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "notify"`)

	stdout, stderr, _ := runBinary(t, t.TempDir(),
		startEnv(t, srv, "LSTK_UPDATE_CHECK=off"), bin,
		"--config", configFile, "start", "--non-interactive")

	assert.Equal(t, int32(0), hits.Load(), "the env var must win over the config key")
	assert.NotContains(t, stdout+stderr, "Update available")
}

// The note must name the external manager rather than advising `lstk update`,
// which refuses on such an install.
func TestUpdateCheckNoteNamesExternalManager(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, ".local/share/mise/installs/github-localstack-lstk/latest", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "notify"`)

	stdout, stderr, _ := runBinary(t, t.TempDir(), startEnv(t, srv), bin,
		"--config", configFile, "start", "--non-interactive")

	combined := stdout + stderr
	assert.Contains(t, combined, "mise")
	assert.NotContains(t, combined, "run lstk update")
}

func TestInvalidUpdateCheckEnvVarIsRejectedAsConfigInvalid(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "notify"`)

	stdout, _, err := runBinary(t, t.TempDir(),
		startEnv(t, srv, "LSTK_UPDATE_CHECK=quiet"), bin,
		"--config", configFile, "start", "--non-interactive", "--json")

	requireExitCode(t, 1, err)
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %s", stdout)
	assert.Equal(t, "CONFIG_INVALID", envelope.Error.Code)
}

func TestInvalidUpdateCheckInConfigIsConfigInvalidUnderJSON(t *testing.T) {
	t.Parallel()

	configFile := writeConfigWithCLI(t, `update_check = "quiet"`)
	stdout, _, err := runLstk(t, testContext(t), t.TempDir(),
		append(testEnvWithHome(t.TempDir(), ""), unreachableDockerHost),
		"--config", configFile, "start", "--non-interactive", "--json")

	requireExitCode(t, 1, err)
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %s", stdout)
	assert.Equal(t, "CONFIG_INVALID", envelope.Error.Code)
}

// The "Never ask again" option writes config, so it must only appear when
// there is a config file to write to. On a genuine first run config.toml does
// not exist yet — it is created later, by the emulator picker — so offering it
// there would tell the user a preference was saved when it was dropped.
func TestUpdatePromptOmitsNeverAskAgainOnFirstRun(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")

	// No --config and a fresh HOME: config.toml does not exist.
	cmd := exec.Command(bin, "start")
	cmd.Env = startEnv(t, srv)
	proc := startCmdInPTY(t, testContext(t), cmd)
	t.Cleanup(proc.kill)

	// The prompt is the first thing ui.Run emits, ahead of the Docker health
	// check, so no emulator is needed to observe it.
	proc.waitForOutput("Update lstk to latest version?", "the update prompt should appear")
	out := proc.output()
	assert.Contains(t, out, "Update now")
	assert.Contains(t, out, "Remind me next time")
	assert.NotContains(t, out, "Never ask again", "the opt-out must be absent with no config file")
}

func TestUpdatePromptOffersNeverAskAgainWhenConfigExists(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	srv := countingReleaseServer(t, "v9.9.9", &hits)
	bin := buildStampedLstk(t, "opt/bin", "0.0.1")
	configFile := writeConfigWithCLI(t, `update_check = "prompt"`)

	cmd := exec.Command(bin, "--config", configFile, "start")
	cmd.Env = startEnv(t, srv)
	proc := startCmdInPTY(t, testContext(t), cmd)
	t.Cleanup(proc.kill)

	proc.waitForOutput("Update lstk to latest version?", "the update prompt should appear")
	proc.waitForOutput("Never ask again", "the opt-out must be offered when config exists")
}

// --check reports whether a newer version exists and writes nothing, so it is
// exempt from the refusal. Without the exemption this exits 1 on every
// externally-managed install and every unwritable install directory.
func TestUpdateCheckIsNotRefusedOnExternalInstall(t *testing.T) {
	t.Parallel()

	bin := installLstkUnder(t, ".local/share/mise/installs/github-localstack-lstk/latest")
	stdout, stderr, err := runBinary(t, t.TempDir(), testEnvWithHome(t.TempDir(), ""), bin, "update", "--check")

	require.NoError(t, err, stderr)
	assert.NotContains(t, stdout+stderr, "will not update itself")
}
