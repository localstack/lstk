package awscli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSpanName(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"s3", "ls"}, "lstk aws s3 ls"},
		{[]string{"lambda", "invoke", "--function-name", "foo"}, "lstk aws lambda invoke"},
		{[]string{"sqs", "send-message", "--queue-url", "http://..."}, "lstk aws sqs send-message"},
		{[]string{"--region", "eu-west-1", "s3", "ls"}, "lstk aws s3 ls"},
		{[]string{"s3"}, "lstk aws s3"},
		{[]string{}, "lstk aws"},
		{[]string{"--version"}, "lstk aws"},
	}
	for _, c := range cases {
		got := spanName(c.args)
		assert.Equal(t, c.want, got, "spanName(%v)", c.args)
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
