package azurecli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecEnvAppendsExtraEnvAfterBase(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AZURE_CONFIG_DIR=/global"}
	got := execEnv(base, []string{"AZURE_CONFIG_DIR=/isolated"})

	// os/exec keeps the last occurrence, so extraEnv must come after base for
	// the isolated AZURE_CONFIG_DIR to win over an inherited one.
	assert.Equal(t, []string{"PATH=/usr/bin", "AZURE_CONFIG_DIR=/global", "AZURE_CONFIG_DIR=/isolated", "PYTHONUNBUFFERED=1"}, got)
}

func TestExecEnvForcesUnbufferedPython(t *testing.T) {
	assert.Contains(t, execEnv([]string{"PATH=/usr/bin"}, nil), "PYTHONUNBUFFERED=1")
}

func TestExecEnvPreservesUserPythonUnbuffered(t *testing.T) {
	fromBase := execEnv([]string{"PYTHONUNBUFFERED=x"}, nil)
	assert.Contains(t, fromBase, "PYTHONUNBUFFERED=x")
	assert.NotContains(t, fromBase, "PYTHONUNBUFFERED=1")

	fromExtra := execEnv([]string{"PATH=/usr/bin"}, []string{"PYTHONUNBUFFERED=x"})
	assert.Contains(t, fromExtra, "PYTHONUNBUFFERED=x")
	assert.NotContains(t, fromExtra, "PYTHONUNBUFFERED=1")
}

func TestExecEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	extra := []string{"AZURE_CONFIG_DIR=/isolated"}

	execEnv(base, extra)

	assert.Equal(t, []string{"PATH=/usr/bin"}, base)
	assert.Equal(t, []string{"AZURE_CONFIG_DIR=/isolated"}, extra)
}
