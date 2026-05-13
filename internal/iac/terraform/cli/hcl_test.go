package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseS3BackendBlock(t *testing.T) {
	dir := t.TempDir()
	tf := `
terraform {
  backend "s3" {
    bucket         = "my-state"
    key            = "path/to/state.tfstate"
    region         = "eu-west-1"
    dynamodb_table = "tf-lock"
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(tf), 0o644))

	cfg, err := parseS3Backend(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "my-state", cfg.Bucket)
	assert.Equal(t, "path/to/state.tfstate", cfg.Key)
	assert.Equal(t, "eu-west-1", cfg.Region)
	assert.Equal(t, "tf-lock", cfg.DynamoDBTable)
}

func TestParseS3BackendNoBackend(t *testing.T) {
	dir := t.TempDir()
	tf := `
provider "aws" {
  region = "us-east-1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.tf"), []byte(tf), 0o644))

	cfg, err := parseS3Backend(dir)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseS3BackendEmptyDirectory(t *testing.T) {
	cfg, err := parseS3Backend(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestParseS3BackendSkipsUnparseableFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.tf"), []byte("this is not valid hcl {{{"), 0o644))
	good := `
terraform {
  backend "s3" {
    bucket = "ok"
    key    = "k"
  }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "good.tf"), []byte(good), 0o644))

	cfg, err := parseS3Backend(dir)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "ok", cfg.Bucket)
}
