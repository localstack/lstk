package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocsCommandGeneratesManPages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "manpages")

	_, stderr, err := runLstk(t, testContext(t), "", os.Environ(), "docs", "--format", "man", "--dir", dir)
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assert.FileExists(t, filepath.Join(dir, "lstk.1"))
	assert.FileExists(t, filepath.Join(dir, "lstk-start.1"))
	assert.FileExists(t, filepath.Join(dir, "lstk-stop.1"))
}

func TestDocsCommandGeneratesMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "markdown")

	_, stderr, err := runLstk(t, testContext(t), "", os.Environ(), "docs", "--format", "markdown", "--dir", dir)
	require.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	assert.FileExists(t, filepath.Join(dir, "lstk.md"))
	assert.FileExists(t, filepath.Join(dir, "lstk_start.md"))
	assert.FileExists(t, filepath.Join(dir, "lstk_stop.md"))
}

func TestDocsCommandRejectsInvalidFormat(t *testing.T) {
	dir := t.TempDir()

	_, _, err := runLstk(t, testContext(t), "", os.Environ(), "docs", "--format", "invalid", "--dir", dir)
	require.Error(t, err)
	requireExitCode(t, 1, err)
}

func TestDocsCommandIsHidden(t *testing.T) {
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), os.Environ(), "--help")
	require.NoError(t, err, stderr)

	assert.NotContains(t, stdout, "docs")
}
