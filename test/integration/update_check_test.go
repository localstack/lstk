package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateCheckConfig is a config file carrying distinctive comments, so a test
// that triggers a config write can also prove the write preserved them.
const updateCheckConfig = `# User-maintained lstk config
[[containers]]
type = "aws"     # Emulator type
tag  = "latest"  # Docker image tag
port = "4566"    # Host port
`

// writeUpdateCheckConfig writes a config file with the given extra lines
// appended (e.g. a [cli] section) and returns its path.
func writeUpdateCheckConfig(t *testing.T, extra string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(updateCheckConfig+extra), 0o644))
	return path
}

// writeUpdateCheckConfigWithMode writes a config whose [cli] section sets
// update_check to the given raw value.
func writeUpdateCheckConfigWithMode(t *testing.T, mode string) string {
	t.Helper()
	return writeUpdateCheckConfig(t, fmt.Sprintf("\n[cli]\nupdate_check = %q\n", mode))
}

// assertConfigCommentsPreserved checks that a config rewrite left the
// user-maintained parts of updateCheckConfig untouched.
func assertConfigCommentsPreserved(t *testing.T, configStr string) {
	t.Helper()
	assert.Contains(t, configStr, "# User-maintained lstk config", "file header comment should be preserved")
	assert.Contains(t, configStr, "# Emulator type", "inline comments should be preserved")
	assert.Contains(t, configStr, `port = "4566"`, "existing config values should be preserved")
}

// updateCheckEnv builds the environment the update-check tests run in: a mock
// GitHub advertising v0.0.2, and Docker unreachable so the run fails fast right
// after the update check instead of needing a daemon. It returns the environment
// and the mock's request counter.
func updateCheckEnv(t *testing.T, extraEnv ...string) ([]string, *atomic.Int64) {
	t.Helper()
	srv, requests := mockGitHubReleaseServerCounting(t, "v0.0.2", nil)
	environ := append(mockGitHubEnv(t, srv), unreachableDockerHost)
	return append(environ, extraEnv...), requests
}

// startUpdateCheckRun runs a version-stamped lstk in a PTY in that environment.
func startUpdateCheckRun(t *testing.T, binPath, configFile string, extraEnv ...string) (*ptyProc, *atomic.Int64) {
	t.Helper()

	environ, requests := updateCheckEnv(t, extraEnv...)

	// Long enough for the check plus the Docker failure, short enough that a
	// regression which blocks on an unanswerable prompt fails rather than hangs.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, binPath, "--config", configFile)
	cmd.Env = environ
	return startCmdInPTY(t, ctx, cmd), requests
}

// assertNoUpdateCheck asserts a run said nothing about updates and made no
// request for release metadata.
func assertNoUpdateCheck(t *testing.T, out string, requests *atomic.Int64) {
	t.Helper()
	assert.NotContains(t, out, "Update available")
	assert.NotContains(t, out, "New lstk version available")
	assert.Zero(t, requests.Load(), "the check must not contact GitHub at all")
}

// TestUpdateCheckModeOff is the reason DEVX-1029 exists: a permanent opt-out.
// "off" must suppress the notice *and* the network request.
func TestUpdateCheckModeOff(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "bin")
	configFile := writeUpdateCheckConfigWithMode(t, "off")

	p, requests := startUpdateCheckRun(t, binPath, configFile)
	out, _ := p.wait()

	assertNoUpdateCheck(t, out, requests)
}

// TestUpdateCheckModeNotify covers the middle ground the reporter asked for: a
// one-line hint that never waits for input. The run reaching the Docker failure
// without any keypress is the proof it did not block.
func TestUpdateCheckModeNotify(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "bin")
	configFile := writeUpdateCheckConfigWithMode(t, "notify")

	p, _ := startUpdateCheckRun(t, binPath, configFile)
	out, _ := p.wait()

	assert.Contains(t, out, "Update available: 0.0.1 → v0.0.2 (run lstk update)")
	assert.NotContains(t, out, "Update lstk to latest version?", "notify mode must not prompt")
}

// TestUpdateCheckEnvOverridesConfig pins the documented precedence: the env var
// wins, in both directions.
func TestUpdateCheckEnvOverridesConfig(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "bin")

	t.Run("env prompt beats config off", func(t *testing.T) {
		t.Parallel()
		configFile := writeUpdateCheckConfigWithMode(t, "off")

		p, _ := startUpdateCheckRun(t, binPath, configFile, string(env.UpdateCheck)+"=prompt")
		p.waitForOutput("Update lstk to latest version?", "the env var should re-enable the prompt")
		p.write("r")
		_, _ = p.wait()
	})

	t.Run("env off beats config prompt", func(t *testing.T) {
		t.Parallel()
		configFile := writeUpdateCheckConfigWithMode(t, "prompt")

		p, requests := startUpdateCheckRun(t, binPath, configFile, string(env.UpdateCheck)+"=off")
		out, _ := p.wait()

		assertNoUpdateCheck(t, out, requests)
	})
}

// TestUpdateCheckInvalidValueWarnsAndStarts guards the decision that a typo in
// the setting must not stop lstk: it is reported and ignored.
func TestUpdateCheckInvalidValueWarnsAndStarts(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "bin")
	configFile := writeUpdateCheckConfigWithMode(t, "yes")

	p, _ := startUpdateCheckRun(t, binPath, configFile)
	p.waitForOutput(`Ignoring update_check in [cli]: invalid update_check value "yes" (must be one of: prompt, notify, off)`,
		"an unparsable value should be reported, not silently ignored")
	// Falls back to the default policy rather than skipping the check, so the
	// prompt still appears and has to be answered.
	p.waitForOutput("New lstk version available", "an invalid value must not disable the check")
	p.write("r")
	_, _ = p.wait()
}

// TestUpdateNotifiesExternallyManagedInstallByDefault covers the reporter's
// actual setup: an lstk that mise owns, with nothing configured. It must not
// block, and it must point at mise rather than at `lstk update`, which refuses.
func TestUpdateNotifiesExternallyManagedInstallByDefault(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "mise", "installs", "lstk", "0.0.1")
	configFile := writeUpdateCheckConfig(t, "")

	p, _ := startUpdateCheckRun(t, binPath, configFile)
	out, _ := p.wait()

	assert.Contains(t, out, "Update available: 0.0.1 → v0.0.2 (installed with mise — run mise upgrade lstk)")
	assert.NotContains(t, out, "Update lstk to latest version?", "an externally managed install must not be prompted")
}

// TestUpdateCheckDontAskAgain is the in-flow opt-out: a user who is being
// nagged can turn the prompt off without reading any docs, and it must actually
// take effect on the next run.
func TestUpdateCheckDontAskAgain(t *testing.T) {
	t.Parallel()

	binPath := lstkAtInstallPath(t, testContext(t), "0.0.1", "bin")
	configFile := writeUpdateCheckConfig(t, "")

	p, _ := startUpdateCheckRun(t, binPath, configFile)
	p.waitForOutput("New lstk version available", "the prompt should appear with nothing configured")
	require.Contains(t, p.output(), "Don't ask again", "the prompt should offer a durable opt-out")
	p.write("n")
	out, _ := p.wait()

	assert.Contains(t, out, "Won't ask again")

	configData, err := os.ReadFile(configFile)
	require.NoError(t, err)
	configStr := string(configData)
	assert.Contains(t, configStr, "update_check", "the choice should be persisted")
	assert.Contains(t, configStr, "notify", `the choice should persist "notify", not "off"`)
	assertConfigCommentsPreserved(t, configStr)

	// The point of persisting: the next run notifies instead of asking.
	second, _ := startUpdateCheckRun(t, binPath, configFile)
	secondOut, _ := second.wait()
	assert.Contains(t, secondOut, "Update available: 0.0.1 → v0.0.2 (run lstk update)")
	assert.NotContains(t, secondOut, "Update lstk to latest version?")
}

// TestUpdateNotificationHonorsSkippedVersionNonInteractive covers a bug the
// shared-policy refactor fixes: the non-interactive path built its own
// NotifyOptions and so ignored a skipped version entirely.
func TestUpdateNotificationHonorsSkippedVersionNonInteractive(t *testing.T) {
	t.Parallel()
	ctx := testContext(t)

	binPath := lstkAtInstallPath(t, ctx, "0.0.1", "bin")
	configFile := writeUpdateCheckConfig(t, "\n[cli]\nupdate_skipped_version = \"v0.0.2\"\n")

	environ, _ := updateCheckEnv(t)
	stdout, _, _ := runBinary(t, "", environ, binPath, "--config", configFile, "--non-interactive")

	// Exits non-zero because Docker is unreachable; what matters is that the
	// skipped version was honored on the way there.
	assert.NotContains(t, stdout, "Update available", "a skipped version must stay suppressed non-interactively too")
}
