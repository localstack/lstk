package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/localstack/lstk/internal/log"
)

func TestParseRemoteStatesS3WithWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "network" {
  backend   = "s3"
  workspace = "prod"
  config = {
    bucket = "shared-state"
    key    = "network/terraform.tfstate"
    region = "eu-west-1"
  }
}
`)
	states := parseRemoteStates(dir, log.Nop())
	require.Len(t, states, 1)
	assert.Equal(t, "network", states[0].name)
	assert.Equal(t, `"prod"`, states[0].workspace)
	assert.Equal(t, "shared-state", stringAttr(states[0].config, "bucket"))
}

func TestParseRemoteStatesPreservesWorkspaceReference(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "net" {
  backend   = "s3"
  workspace = terraform.workspace
  config = {
    bucket = "b"
    key    = "k"
  }
}
`)
	states := parseRemoteStates(dir, log.Nop())
	require.Len(t, states, 1)
	assert.Equal(t, "terraform.workspace", states[0].workspace)
}

func TestParseRemoteStatesMinimal(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "min" {
  backend = "s3"
  config = {
    bucket = "b"
    key    = "k"
  }
}
`)
	states := parseRemoteStates(dir, log.Nop())
	require.Len(t, states, 1)
	assert.Empty(t, states[0].workspace)
}

func TestParseRemoteStatesNonS3Ignored(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "other" {
  backend = "remote"
  config = {
    organization = "acme"
  }
}
`)
	assert.Empty(t, parseRemoteStates(dir, log.Nop()))
}

func TestGenerateOverrideRemoteStateModern(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" {}`)
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "network" {
  backend   = "s3"
  workspace = "prod"
  config = {
    bucket = "shared-state"
    key    = "network/terraform.tfstate"
    region = "eu-west-1"
  }
}
`)
	opts := baseOpts(dir)
	opts.remoteStates = parseRemoteStates(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.Contains(t, out, `data "terraform_remote_state" "network" {`)
	assert.Contains(t, out, `backend = "s3"`)
	assert.Contains(t, out, `workspace = "prod"`)
	// Full config reproduced.
	assert.Contains(t, out, `bucket = "shared-state"`)
	assert.Contains(t, out, `key = "network/terraform.tfstate"`)
	assert.Contains(t, out, `region = "eu-west-1"`)
	// Endpoints injected.
	assert.Contains(t, out, "endpoints = {")
	assert.Contains(t, out, `access_key = "test"`)
}

func TestGenerateOverrideRemoteStateNonS3NotRegenerated(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" {}`)
	writeTF(t, dir, "remote.tf", `
data "terraform_remote_state" "other" {
  backend = "remote"
  config = { organization = "acme" }
}
`)
	opts := baseOpts(dir)
	opts.remoteStates = parseRemoteStates(dir, log.Nop())
	paths, err := generateOverride(opts)
	require.NoError(t, err)

	out := readOverride(t, paths)
	assert.NotContains(t, out, "terraform_remote_state")
}
