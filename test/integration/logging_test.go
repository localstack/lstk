package integration_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogging_NonTTY_WritesToLogFile(t *testing.T) {
	t.Parallel()

	tmpHome := t.TempDir()
	var logPath string
	if runtime.GOOS == "windows" {
		logPath = filepath.Join(tmpHome, "AppData", "Roaming", "lstk", "lstk.log")
	} else {
		require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
		logPath = filepath.Join(tmpHome, ".config", "lstk", "lstk.log")
	}
	e := testEnvWithHome(tmpHome, "")

	ctx := testContext(t)
	_, _, err := runLstk(t, ctx, "", e, "--version")
	require.NoError(t, err)

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[INFO] lstk")
	assert.Contains(t, string(logContents), "starting")
}

func TestLogging_TTY_WritesToLogFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("PTY not supported on Windows")
	}

	tmpHome := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmpHome, ".config"), 0755))
	logPath := filepath.Join(tmpHome, ".config", "lstk", "lstk.log")
	e := testEnvWithHome(tmpHome, "")

	ctx := testContext(t)
	_, err := runLstkInPTY(t, ctx, e, "--version")
	require.NoError(t, err)

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[INFO] lstk")
	assert.Contains(t, string(logContents), "starting")
}
