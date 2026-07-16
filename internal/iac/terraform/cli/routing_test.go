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

// DEVX-1002 — help flags (in any position) are unproxied and never require
// the emulator, even in an uninitialized project. Unlike aws/cdk, terraform
// has no bare `help` pseudo-subcommand (`terraform help` is an error), so it
// is not recognized.
func TestIsHelp(t *testing.T) {
	trueCases := [][]string{
		{"--help"}, {"-h"}, {"-help"},
		{"plan", "--help"}, {"plan", "-h"},
	}
	for _, args := range trueCases {
		assert.Truef(t, IsHelp(args), "%v", args)
		assert.Truef(t, IsUnproxied(args), "%v", args)
	}

	falseCases := [][]string{{"plan"}, {"apply", "-auto-approve"}, {"init"}, {"help"}, {"help", "plan"}}
	for _, args := range falseCases {
		assert.Falsef(t, IsHelp(args), "%v", args)
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

	// help requests never require the emulator, even with an S3 backend present.
	assert.False(t, RequiresEmulator([]string{"--help"}, withBackend, log.Nop()))
	assert.False(t, RequiresEmulator([]string{"plan", "-h"}, withBackend, log.Nop()))
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
