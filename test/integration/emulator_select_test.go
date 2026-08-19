package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoEmulatorSelectionWhenConfigExists(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	// Pre-create the config so lstk does not treat this as a first run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	assert.Never(t, func() bool {
		return strings.Contains(p.output(), "Which emulator would you like to use?")
	}, 2*time.Second, 100*time.Millisecond, "emulator selection prompt should not appear when config already exists")

	p.kill()
}

func TestFirstRunShowsEmulatorSelectionPrompt(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	// Confirm no config exists at the path lstk would use — this is what triggers first-run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	p.waitForOutput("Which emulator would you like to use?", "emulator selection prompt should appear on first run")

	// Each choice is a selectable row advertising the key that picks it directly.
	// The shortcut is derived from the option's key by output.OptionLabel, so a
	// picker whose labels are bare names still tells the user what to press.
	p.waitForOutput("[A] AWS", "each emulator row should advertise its shortcut")
	p.waitForOutput("[Z] Azure", "each emulator row should advertise its shortcut")

	// Confirm the default-highlighted option (AWS) by pressing Enter.
	p.write("\r")

	p.waitForOutput("AWS emulator selected.", "selection confirmation should appear after pressing Enter")

	// SetEmulatorType writes the config before emitting the confirmation message,
	// so the file is guaranteed to exist and contain the selection by this point.
	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configData), `type = "aws"`)

	p.kill()
}

// Running an unrelated command (one that doesn't itself start the emulator)
// before ever running `lstk start` must not silently lock in a default
// emulator. Every command other than bare `lstk`/`lstk start` defers config
// creation (initConfigDeferCreate), so it must not write config.toml on its
// own first run — otherwise firstRun would be false by the time the user
// finally runs `lstk start`, and the selector would never appear.
func TestFirstRunStillShowsSelectionPromptAfterRunningAnotherCommand(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	// Run a command that doesn't start the emulator — this used to eagerly
	// create the default (type = "aws") config via config.Init, consuming
	// firstRun before the user ever saw the selector.
	_, _, err = runLstk(t, testContext(t), "", e, "volume", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath, "running an unrelated command must not create the default config")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	p.waitForOutput("Which emulator would you like to use?",
		"emulator selection prompt should still appear on first `start` even after running another command first")

	p.kill()
}

func TestFirstRunCanSelectAzureEmulator(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	p.waitForOutput("Which emulator would you like to use?", "emulator selection prompt should appear on first run")

	assert.Contains(t, p.output(), "Azure", "Azure should be offered as a selectable emulator")

	// Press the Azure selection key ('z') instead of the default-highlighted AWS.
	p.write("z")

	p.waitForOutput("Azure emulator selected.", "Azure selection confirmation should appear")

	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(configData), `type = "azure"`)

	p.kill()
}

func TestFirstRunPromptsForLoginBeforeEmulatorSelection(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		Without(env.AuthToken).
		With(env.APIEndpoint, mockServer.URL).
		With(env.DisableEvents, "1")

	// No config exists so this is a first run; no token means login fires before emulator selection.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	p.waitForOutput("Press any key when complete", "auth prompt should appear on first run when no token is set")

	assert.NotContains(t, p.output(), "Which emulator would you like to use?",
		"emulator selection prompt must not appear before auth completes")

	p.write("\r")

	p.waitForOutput("Which emulator would you like to use?", "emulator selection prompt should appear after auth completes")

	p.kill()
}

func TestFirstRunNonInteractiveEmitsDefaultEmulatorNote(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).With(env.DisableEvents, "1")

	// Verify no config exists — this is what triggers first-run.
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	// Process fails at container.Start (no Docker), but the note is emitted before that.
	stdout, _, runErr := runLstk(t, testContext(t), "", e.With(env.AuthToken, "test-token"), "--non-interactive")
	assert.Error(t, runErr, "expected failure: no Docker available")
	assert.Contains(t, stdout, "Configured with default emulator", "non-interactive first run should note the default emulator")
}

// A first run that fails before doing any work (no Docker) leaves no config, so
// the next run is still a first run and shows the selector instead of defaulting.
func TestEmulatorSelectionReappearsAfterFailedFirstRun(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	base := env.Environ(testEnvWithHome(tmpHome, tmpHome)).With(env.DisableEvents, "1")

	configPath, _, err := runLstk(t, testContext(t), "", base, "config", "path")
	require.NoError(t, err)
	require.NoFileExists(t, configPath)

	noDocker := base.With(env.Key("DOCKER_HOST"), "tcp://localhost:1")
	stdout, _, runErr := runLstk(t, testContext(t), "", noDocker, "--non-interactive")
	require.Error(t, runErr, "first run should fail when Docker is unavailable")
	assert.Contains(t, stdout, "Docker is not available")
	require.NoFileExists(t, configPath, "a run that fails before doing any work must not create a config")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, base, "start")

	p.waitForOutput("Which emulator would you like to use?",
		"emulator selection prompt should reappear after a first run that failed before selection")

	p.kill()
}

// Deleting the config directory after a successful run must trigger the emulator
// selector again on the next run — the selector is gated on the config file being
// absent, so the directory alone must not count as "already configured".
func TestEmulatorSelectionReappearsAfterConfigDirDeleted(t *testing.T) {
	requireDocker(t)
	t.Parallel()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		With(env.DisableEvents, "1").
		With(env.AuthToken, "test-token")

	// Resolve where lstk would create the config, then pre-create it so lstk
	// believes this is not a first run (simulates a previous successful start).
	configPath, _, err := runLstk(t, testContext(t), "", e, "config", "path")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0755))
	require.NoError(t, os.WriteFile(configPath, []byte("[[containers]]\ntype = \"aws\"\ntag = \"latest\"\nport = \"4566\"\n"), 0644))
	require.FileExists(t, configPath)

	// Delete the entire config directory — this is what the user reported.
	require.NoError(t, os.RemoveAll(filepath.Dir(configPath)))
	require.NoFileExists(t, configPath)

	// The next run must show the emulator selector again, not silently default to AWS.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	p := startLstkInPTY(t, ctx, e, "start")

	p.waitForOutput("Which emulator would you like to use?",
		"emulator selection prompt should reappear after the config directory is deleted")

	p.kill()
}

func TestFirstRunChecksDockerBeforeAuthAndSelection(t *testing.T) {
	t.Parallel()

	mockServer := createMockAPIServer(t, "test-license-token", true)
	defer mockServer.Close()

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).
		Without(env.AuthToken).
		With(env.APIEndpoint, mockServer.URL).
		With(env.DisableEvents, "1").
		With(env.Key("DOCKER_HOST"), "tcp://localhost:1")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	out, err := runLstkInPTY(t, ctx, e, "start")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, out, "Docker is not available")
	assert.NotContains(t, out, "Press any key when complete",
		"login prompt must not appear when the runtime is unavailable")
	assert.NotContains(t, out, "Which emulator would you like to use?",
		"emulator selection must not appear when the runtime is unavailable")
}
