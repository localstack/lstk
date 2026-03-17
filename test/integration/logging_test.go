package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogging_NonTTY_WritesToLogFile(t *testing.T) {
	logPath := filepath.Join(configDir(), "lstk.log")
	_ = os.Remove(logPath)

	ctx := testContext(t)
	_, _, err := runLstk(t, ctx, "", nil, "--version")
	require.NoError(t, err)

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[INFO] lstk")
	assert.Contains(t, string(logContents), "starting")
}

func TestLogging_TTY_WritesToLogFile(t *testing.T) {
	logPath := filepath.Join(configDir(), "lstk.log")
	_ = os.Remove(logPath)

	ctx := testContext(t)
	_, err := runLstkInPTY(t, ctx, nil, "--version")
	require.NoError(t, err)

	logContents, err := os.ReadFile(logPath)
	require.NoError(t, err, "expected lstk.log to be created at %s", logPath)
	assert.Contains(t, string(logContents), "[INFO] lstk")
	assert.Contains(t, string(logContents), "starting")
}
