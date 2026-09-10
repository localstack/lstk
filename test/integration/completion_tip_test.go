package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionTipText is asserted verbatim, "> Tip: " prefix included: that
// prefix is the convention the other post-start tips use (tipsForType in
// internal/container/tips.go), so it is observable behavior, not styling.
const completionTipText = "> Tip: Set up tab completion: lstk completion"

// firstRunHome returns an isolated home with no lstk config, so the run under
// test is a first run (config.toml absent is what firstRun means).
func firstRunHome(t *testing.T) (env.Environ, string) {
	t.Helper()

	tmpHome := t.TempDir()
	scheduleVolumeCleanup(t, tmpHome)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath, "test setup: config must be absent for this to be a first run")

	return e, configPath
}

// distinctTips returns the unique "> Tip:" lines in out. Bubble Tea repaints
// lines, so a raw occurrence count would overstate.
func distinctTips(out string) []string {
	var tips []string
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, "> Tip:")
		if i < 0 {
			continue
		}
		tip := strings.TrimSpace(line[i:])
		if !slices.Contains(tips, tip) {
			tips = append(tips, tip)
		}
	}
	return tips
}

func TestFirstRunShowsCompletionTip(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e, _ := firstRunHome(t)

	p := startLstkInPTY(t, testContext(t), e.With(env.APIEndpoint, mockServer.URL), "start")

	// First run shows the emulator picker; accept the default (AWS).
	p.waitForOutput("Which emulator would you like to use?", "emulator selection prompt should appear on first run")
	p.write("\r")

	// Post-start setup asks about the AWS CLI profile; decline it.
	p.waitForOutputTimeout(awsSetupPrompt, 2*time.Minute, "container should become ready")
	p.write("n")

	out, err := p.wait()
	require.NoError(t, err, "lstk start should exit successfully")

	// Exactly one tip, and it is this one — two tips compete and neither lands (#484).
	assert.Equal(t, []string{completionTipText}, distinctTips(out),
		"first successful interactive start should point at shell completion setup, and show no other tip")
}

// --type answers the first-run picker, so it suppresses it — but the run is
// still a first run and the tip must survive that.
func TestFirstRunWithEmulatorTypeFlagShowsCompletionTip(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e, _ := firstRunHome(t)

	p := startLstkInPTY(t, testContext(t), e.With(env.APIEndpoint, mockServer.URL), "start", "--type", "aws")

	assert.NotContains(t, p.output(), "Which emulator would you like to use?",
		"--type should answer the picker rather than showing it")

	p.waitForOutputTimeout(awsSetupPrompt, 2*time.Minute, "container should become ready")
	p.write("n")

	out, err := p.wait()
	require.NoError(t, err, "lstk start --type aws should exit successfully")

	assert.Contains(t, out, completionTipText, "--type must not suppress the first-run tip along with the picker")
}

func TestSubsequentRunDoesNotShowCompletionTip(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e, configPath := firstRunHome(t)

	// Pre-create the config so this is no longer a first run.
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath,
		[]byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	p := startLstkInPTY(t, testContext(t), e.With(env.APIEndpoint, mockServer.URL), "start")

	p.waitForOutputTimeout(awsSetupPrompt, 2*time.Minute, "container should become ready")
	p.write("n")

	out, err := p.wait()
	require.NoError(t, err, "lstk start should exit successfully")

	assert.NotContains(t, out, completionTipText, "the tip must not repeat once lstk has been configured")
	assert.Len(t, distinctTips(out), 1, "a configured run should show the rotating tip, and only that")
}

func TestFirstRunNonInteractiveShowsNoCompletionTip(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e, _ := firstRunHome(t)

	stdout, stderr, err := runLstk(t, testContext(t), "", e.With(env.APIEndpoint, mockServer.URL), "start")
	require.NoError(t, err, "lstk start failed: %s", stderr)

	assert.Contains(t, stdout, "Configured with default emulator", "test setup: this should be a first run")
	assert.NotContains(t, stdout, completionTipText, "a tip is noise in non-interactive output")
}

func TestFirstRunJSONEnvelopeHasNoCompletionTip(t *testing.T) {
	requireDocker(t)
	_ = env.Require(t, env.AuthToken)

	cleanup()
	t.Cleanup(cleanup)

	mockServer := createMockLicenseServer(true)
	defer mockServer.Close()

	e, _ := firstRunHome(t)

	stdout, stderr, err := runLstk(t, testContext(t), "", e.With(env.APIEndpoint, mockServer.URL), "start", "--json")
	require.NoError(t, err, "lstk start --json failed: %s", stderr)

	// A distinctive substring, not completionTipText: inside a JSON string,
	// escaping could break an exact match and hide the leak.
	assert.NotContains(t, stdout, "tab completion", "the tip must never reach machine-readable output")

	envelope := decodeEnvelope(t, stdout)
	assert.Equal(t, "ok", envelope.Status)
	var data startJSONData
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	assert.Equal(t, "aws", data.Emulator, "test setup: the first run should have started the default emulator")
}
