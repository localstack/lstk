package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/log"
)

// 9.1 — the unconditionally-unproxied set is exactly fmt/validate/version, and
// init routing depends on whether an S3 backend is present.
func TestIsUnproxiedSet(t *testing.T) {
	for _, sub := range []string{"fmt", "validate", "version"} {
		assert.True(t, IsUnproxied([]string{sub}), "%s should be unproxied", sub)
	}
	for _, sub := range []string{"init", "plan", "apply", "destroy", "providers"} {
		assert.False(t, IsUnproxied([]string{sub}), "%s should not be unconditionally unproxied", sub)
	}
}

func TestRequiresEmulatorRouting(t *testing.T) {
	noBackend := t.TempDir()
	writeTF(t, noBackend, "main.tf", `provider "aws" {}`)

	withBackend := t.TempDir()
	writeTF(t, withBackend, "backend.tf", "\nterraform {\n  backend \"s3\" {\n    bucket = \"b\"\n    key    = \"k\"\n  }\n}\n")

	// fmt/validate/version never require the emulator, regardless of backend.
	for _, sub := range []string{"fmt", "validate", "version"} {
		assert.False(t, RequiresEmulator([]string{sub}, withBackend, log.Nop()), "%s", sub)
	}

	// init depends on backend presence.
	assert.False(t, RequiresEmulator([]string{"init"}, noBackend, log.Nop()), "init without backend passes through")
	assert.True(t, RequiresEmulator([]string{"init"}, withBackend, log.Nop()), "init with backend needs emulator")

	// plan/apply always require the emulator.
	assert.True(t, RequiresEmulator([]string{"plan"}, noBackend, log.Nop()))
	assert.True(t, RequiresEmulator([]string{"apply", "-auto-approve"}, noBackend, log.Nop()))
}

// 9.2 — adding a backend/remote-state section still refuses to clobber a
// pre-existing override file.
func TestGenerateOverrideBackendRefusesPreexistingFile(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", "\nterraform {\n  backend \"s3\" {\n    bucket = \"b\"\n    key    = \"k\"\n  }\n}\n")
	path := filepath.Join(dir, defaultOverrideFileName)
	require.NoError(t, os.WriteFile(path, []byte("# my own file\n"), 0644))

	opts := baseOpts(dir)
	opts.includeProvider = false
	opts.backend = parseS3Backend(dir, log.Nop())
	_, err := generateOverride(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# my own file\n", string(content))
}
