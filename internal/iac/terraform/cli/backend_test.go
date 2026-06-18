package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/log"
)

func TestParseS3BackendAllFields(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", `
terraform {
  backend "s3" {
    bucket               = "my-state"
    key                  = "app/terraform.tfstate"
    region               = "eu-west-1"
    dynamodb_table       = "tf-locks"
    workspace_key_prefix = "envs"
  }
}
`)
	b := parseS3Backend(dir, log.Nop())
	require.NotNil(t, b)
	assert.Equal(t, "my-state", b.bucket)
	assert.Equal(t, "eu-west-1", b.region)
	assert.Equal(t, "tf-locks", b.dynamoDBTable)
	assert.Equal(t, "envs", stringAttr(b.attrs, "workspace_key_prefix"))
}

func TestParseS3BackendRequiredFieldsOnly(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", `
terraform {
  backend "s3" {
    bucket = "minimal"
    key    = "terraform.tfstate"
  }
}
`)
	b := parseS3Backend(dir, log.Nop())
	require.NotNil(t, b)
	assert.Equal(t, "minimal", b.bucket)
	assert.Empty(t, b.region)
	assert.Empty(t, b.dynamoDBTable)
}

func TestParseS3BackendNonS3(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", `
terraform {
  backend "gcs" {
    bucket = "gcs-state"
  }
}
`)
	assert.Nil(t, parseS3Backend(dir, log.Nop()))
	assert.False(t, HasS3Backend(dir, log.Nop()))
}

func TestParseS3BackendAbsent(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" {}`)
	assert.Nil(t, parseS3Backend(dir, log.Nop()))
	assert.False(t, HasS3Backend(dir, log.Nop()))
}

// minimalBackendTF is a valid (newline-separated) minimal S3 backend block.
const minimalBackendTF = `
terraform {
  backend "s3" {
    bucket = "b"
    key    = "k"
  }
}
`

func TestHasS3BackendTrue(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", minimalBackendTF)
	assert.True(t, HasS3Backend(dir, log.Nop()))
}

// The generated override must reproduce the full backend block (user args
// carried forward) plus lstk's managed args, since Terraform replaces backend
// blocks from override files wholesale.
func TestGenerateOverrideBackendModern(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", `
terraform {
  backend "s3" {
    bucket         = "my-state"
    key            = "app/terraform.tfstate"
    region         = "eu-west-1"
    dynamodb_table = "tf-locks"
  }
}
`)
	opts := baseOpts(dir)
	opts.includeProvider = false
	opts.backend = parseS3Backend(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	// Backend-only override: no provider blocks.
	assert.NotContains(t, out, `provider "aws" {`)
	assert.Contains(t, out, `backend "s3" {`)
	// User args preserved.
	assert.Contains(t, out, `bucket = "my-state"`)
	assert.Contains(t, out, `key = "app/terraform.tfstate"`)
	assert.Contains(t, out, `dynamodb_table = "tf-locks"`)
	// User region wins over the resolved fallback.
	assert.Contains(t, out, `region = "eu-west-1"`)
	// lstk managed args.
	assert.Contains(t, out, `access_key = "test"`)
	assert.Contains(t, out, `secret_key = "test"`)
	assert.Contains(t, out, "skip_credentials_validation = true")
	assert.Contains(t, out, "skip_requesting_account_id = true")
	// Modern endpoints map with the full fixed set including sso.
	assert.Contains(t, out, "endpoints = {")
	for _, svc := range []string{"s3", "dynamodb", "iam", "sts", "sso"} {
		assert.Contains(t, out, svc+" = ")
	}
}

func TestGenerateOverrideBackendUsesResolvedRegionFallback(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", minimalBackendTF)
	opts := baseOpts(dir)
	opts.includeProvider = false
	opts.region = "us-west-2"
	opts.backend = parseS3Backend(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, `region = "us-west-2"`)
}

func TestGenerateOverrideBackendLegacy(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", minimalBackendTF)
	opts := baseOpts(dir)
	opts.includeProvider = false
	opts.legacy = true
	// A path-style (IP/localhost) endpoint so force_path_style resolves to true.
	opts.endpointURL = "http://127.0.0.1:4566"
	opts.backend = parseS3Backend(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.NotContains(t, out, "endpoints = {")
	assert.Contains(t, out, `endpoint = `)
	assert.Contains(t, out, `dynamodb_endpoint = `)
	assert.Contains(t, out, `iam_endpoint = `)
	assert.Contains(t, out, `sts_endpoint = `)
	// Legacy has no sso key and uses force_path_style, not use_path_style.
	assert.NotContains(t, out, "sso = ")
	assert.Contains(t, out, "force_path_style = true")
}

func TestGenerateOverrideBackendPathStyleVsVirtualHost(t *testing.T) {
	t.Run("path-style localhost", func(t *testing.T) {
		dir := t.TempDir()
		writeTF(t, dir, "backend.tf", minimalBackendTF)
		opts := baseOpts(dir)
		opts.includeProvider = false
		opts.endpointURL = "http://127.0.0.1:4566"
		opts.backend = parseS3Backend(dir, log.Nop())
		paths, err := generateOverride(opts)
		require.NoError(t, err)
		out := readOverride(t, paths)
		assert.Contains(t, out, "use_path_style = true")
		assert.Contains(t, out, `s3 = "http://127.0.0.1:4566"`)
	})

	t.Run("virtual-host domain", func(t *testing.T) {
		dir := t.TempDir()
		writeTF(t, dir, "backend.tf", minimalBackendTF)
		opts := baseOpts(dir)
		opts.includeProvider = false
		opts.endpointURL = "http://localhost.localstack.cloud:4566"
		opts.backend = parseS3Backend(dir, log.Nop())
		paths, err := generateOverride(opts)
		require.NoError(t, err)
		out := readOverride(t, paths)
		assert.Contains(t, out, "use_path_style = false")
		assert.Contains(t, out, `s3 = "http://s3.localhost.localstack.cloud:4566"`)
		assert.Contains(t, out, `dynamodb = "http://localhost.localstack.cloud:4566"`)
	})
}

// A backend block plus provider blocks coexist in one override for plan/apply.
func TestGenerateOverrideProviderPlusBackend(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" {}`)
	writeTF(t, dir, "backend.tf", minimalBackendTF)
	opts := baseOpts(dir)
	opts.backend = parseS3Backend(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Equal(t, 1, countProviderBlocks(out))
	assert.Contains(t, out, `backend "s3" {`)
}

func TestRenderCtyValue(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "backend.tf", `
terraform {
  backend "s3" {
    bucket  = "b"
    key     = "k"
    encrypt = true
  }
}
`)
	opts := baseOpts(dir)
	opts.includeProvider = false
	opts.backend = parseS3Backend(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)
	out := readOverride(t, paths)
	assert.Contains(t, out, "encrypt = true")
}
