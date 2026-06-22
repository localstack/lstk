package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/log"
)

func baseOpts(workdir string) overrideOptions {
	return overrideOptions{
		workdir:         workdir,
		fileName:        defaultOverrideFileName,
		endpointURL:     "http://localhost.localstack.cloud:4566",
		region:          "us-east-1",
		account:         "test",
		endpointKeys:    []string{"s3", "sqs"},
		includeProvider: true,
		logger:          log.Nop(),
	}
}

func readOverride(t *testing.T, paths []string) string {
	t.Helper()
	require.Len(t, paths, 1)
	content, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	return string(content)
}

func TestGenerateOverrideDefaultProvider(t *testing.T) {
	dir := t.TempDir()
	paths, err := generateOverride(baseOpts(dir))
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, overrideFileMarker)
	assert.Contains(t, out, `provider "aws" {`)
	assert.Contains(t, out, `access_key = "test"`)
	assert.Contains(t, out, `secret_key = "test"`)
	assert.Contains(t, out, `region = "us-east-1"`)
	assert.Contains(t, out, "skip_credentials_validation = true")
	assert.Contains(t, out, "skip_metadata_api_check = true")
	assert.Contains(t, out, "endpoints {")
	assert.Contains(t, out, `s3 = `)
	assert.Contains(t, out, `sqs = "http://localhost.localstack.cloud:4566"`)
	// No .tf files → exactly one (alias-less) block.
	assert.NotContains(t, out, "alias =")
}

func TestGenerateOverrideEncodesRegionAndAccount(t *testing.T) {
	dir := t.TempDir()
	opts := baseOpts(dir)
	opts.region = "us-west-2"
	opts.account = "111111111111"
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, `region = "us-west-2"`)
	assert.Contains(t, out, `access_key = "111111111111"`)
}

func TestGenerateOverridePerAlias(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "aws" {}
provider "aws" {
  alias  = "west"
  region = "us-west-2"
}
`)
	paths, err := generateOverride(baseOpts(dir))
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Equal(t, 2, countProviderBlocks(out))
	assert.Contains(t, out, `alias = "west"`)
}

func TestGenerateOverrideRecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" {}`)
	sub := filepath.Join(dir, "modules", "db")
	require.NoError(t, os.MkdirAll(sub, 0755))
	writeTF(t, sub, "provider.tf", `
provider "aws" {
  alias = "replica"
}
`)
	// A hidden dir (e.g. the .terraform cache) must not be scanned.
	cache := filepath.Join(dir, ".terraform", "providers")
	require.NoError(t, os.MkdirAll(cache, 0755))
	writeTF(t, cache, "cached.tf", `provider "aws" { alias = "should_be_ignored" }`)

	paths, err := generateOverride(baseOpts(dir))
	require.NoError(t, err)

	out := readOverride(t, paths)
	// Default (root) + the aliased provider from the sub-directory.
	assert.Equal(t, 2, countProviderBlocks(out))
	assert.Contains(t, out, `alias = "replica"`)
	assert.NotContains(t, out, "should_be_ignored")
}

func TestGenerateOverrideCustomFileName(t *testing.T) {
	dir := t.TempDir()
	opts := baseOpts(dir)
	opts.fileName = "custom_override.tf"
	paths, err := generateOverride(opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "custom_override.tf"), paths[0])
}

func TestGenerateOverridePathStyleForLocalhost(t *testing.T) {
	dir := t.TempDir()
	opts := baseOpts(dir)
	opts.endpointURL = "http://127.0.0.1:4566"
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, "s3_use_path_style = true")
	assert.Contains(t, out, `s3 = "http://127.0.0.1:4566"`)
}

func TestGenerateOverrideVirtualHostForDomain(t *testing.T) {
	dir := t.TempDir()
	opts := baseOpts(dir)
	opts.endpointURL = "http://localhost.localstack.cloud:4566"
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, "s3_use_path_style = false")
	assert.Contains(t, out, `s3 = "http://s3.localhost.localstack.cloud:4566"`)
	// Non-S3 services keep the bare endpoint.
	assert.Contains(t, out, `sqs = "http://localhost.localstack.cloud:4566"`)
}

func TestGenerateOverrideRefusesPreexistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, defaultOverrideFileName)
	require.NoError(t, os.WriteFile(path, []byte("# my own file\n"), 0644))

	_, err := generateOverride(baseOpts(dir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")

	// The user's file must be untouched.
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# my own file\n", string(content))
}

// A leftover override from a previous interrupted lstk run (carrying lstk's own
// marker) is also refused — lstk keeps no record that it created the file, so it
// must not be silently overwritten or deleted.
func TestGenerateOverrideRefusesOwnLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, defaultOverrideFileName)
	require.NoError(t, os.WriteFile(path, []byte(overrideFileMarker+"\nstale\n"), 0644))

	_, err := generateOverride(baseOpts(dir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite")
}

func writeTF(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
}

func countProviderBlocks(s string) int {
	return strings.Count(s, `provider "aws" {`)
}
