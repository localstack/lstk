package integration_test

import (
	"path/filepath"
	"testing"

	"github.com/localstack/lstk/internal/must"
)

func TestDocsCommandGeneratesManPages(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "manpages")

	_, stderr, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "docs", "--format", "man", "--dir", dir)
	must.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	must.FileExists(t, filepath.Join(dir, "lstk.1"))
	must.FileExists(t, filepath.Join(dir, "lstk-start.1"))
	must.FileExists(t, filepath.Join(dir, "lstk-stop.1"))
}

func TestDocsCommandGeneratesMarkdown(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "markdown")

	_, stderr, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "docs", "--format", "markdown", "--dir", dir)
	must.NoError(t, err, stderr)
	requireExitCode(t, 0, err)

	must.FileExists(t, filepath.Join(dir, "lstk.md"))
	must.FileExists(t, filepath.Join(dir, "lstk_start.md"))
	must.FileExists(t, filepath.Join(dir, "lstk_stop.md"))
}

func TestDocsCommandRejectsInvalidFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, _, err := runLstk(t, testContext(t), "", testEnvWithHome(t.TempDir(), ""), "docs", "--format", "invalid", "--dir", dir)
	must.Error(t, err)
	requireExitCode(t, 1, err)
}

func TestDocsCommandIsHidden(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := runLstk(t, testContext(t), t.TempDir(), testEnvWithHome(t.TempDir(), ""), "--help")
	must.NoError(t, err, stderr)

	must.NotContains(t, stdout, "docs")
}
