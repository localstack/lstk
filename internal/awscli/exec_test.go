package awscli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"s3", "--help"}, {"s3", "ls", "-h"},
		{"help"}, {"s3", "help"},
	} {
		assert.Truef(t, IsHelp(args), "%v", args)
	}
	for _, args := range [][]string{{"s3", "ls"}, {}} {
		assert.Falsef(t, IsHelp(args), "%v", args)
	}
}

func TestBuildEnvSetsDefaultsWhenAbsent(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/user"}
	env := BuildEnv(base)

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
	assert.Contains(t, env, "PATH=/usr/bin")
	assert.Contains(t, env, "HOME=/home/user")
}

func TestBuildEnvPreservesExistingValues(t *testing.T) {
	base := []string{
		"AWS_ACCESS_KEY_ID=custom-key",
		"AWS_SECRET_ACCESS_KEY=custom-secret",
		"AWS_DEFAULT_REGION=eu-west-1",
	}
	env := BuildEnv(base)

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=custom-secret")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=eu-west-1")
	assert.NotContains(t, env, "AWS_ACCESS_KEY_ID=test")
	assert.NotContains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.NotContains(t, env, "AWS_DEFAULT_REGION=us-east-1")
}

func TestBuildEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	original := make([]string, len(base))
	copy(original, base)

	BuildEnv(base)

	assert.Equal(t, original, base)
}

func TestBuildEnvPartialOverride(t *testing.T) {
	base := []string{"AWS_ACCESS_KEY_ID=custom-key"}
	env := BuildEnv(base)

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
}
