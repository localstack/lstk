package integration_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmulatorFlagSwitchesConfigToSnowflake(t *testing.T) {
	t.Parallel()
	// config.SwitchEmulator writes the file before container.Start is called,
	// so we can verify the switch even when the process ultimately fails (no Docker).
	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	e := env.Environ(testEnvWithHome(tmpHome, tmpHome)).With(env.DisableEvents, "1")

	configDir := filepath.Join(tmpHome, ".config", "lstk")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	configPath := filepath.Join(configDir, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(`[[containers]]
type = "aws"
tag  = "latest"
port = "4566"
`), 0644))

	ctx := testContext(t)
	// The process will fail at container.Start (no Docker / no real auth), but the
	// config switch happens earlier so the file should already be updated.
	_, _, _ = runLstk(t, ctx, "", e.With(env.AuthToken, "test-token"), "--emulator", "snowflake", "--non-interactive")

	got, err := os.ReadFile(configPath)
	require.NoError(t, err, "config file should still exist after the run")
	assert.Contains(t, string(got), `type = "snowflake"`, "config should be switched to snowflake")
	assert.NotContains(t, string(got), "\n[[containers]]\ntype = \"aws\"", "original aws block should be commented out")
}

func TestFirstRunShowsEmulatorSelectionPrompt(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

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

	cmd := exec.CommandContext(ctx, binaryPath(), "start")
	cmd.Env = e

	ptmx, err := pty.Start(cmd)
	require.NoError(t, err, "failed to start lstk in PTY")
	defer func() { _ = ptmx.Close() }()

	out := &syncBuffer{}
	outputCh := make(chan struct{})
	go func() {
		_, _ = io.Copy(out, ptmx)
		close(outputCh)
	}()

	require.Eventually(t, func() bool {
		return bytes.Contains(out.Bytes(), []byte("Which emulator would you like to use?"))
	}, 10*time.Second, 100*time.Millisecond, "emulator selection prompt should appear on first run")

	cancel()
	<-outputCh
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
	stdout, _, _ := runLstk(t, testContext(t), "", e.With(env.AuthToken, "test-token"), "--non-interactive")
	assert.Contains(t, stdout, "defaulting to AWS", "non-interactive first run should note the default emulator")
}
