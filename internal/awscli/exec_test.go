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
	env := BuildEnv(base, "")

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
	env := BuildEnv(base, "")

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=custom-secret")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=eu-west-1")
	assert.NotContains(t, env, "AWS_ACCESS_KEY_ID=test")
	assert.NotContains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.NotContains(t, env, "AWS_DEFAULT_REGION=us-east-1")
}

func TestBuildEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AWS_ACCESS_KEY_ID=ambient", "AWS_SESSION_TOKEN=tok"}
	original := make([]string, len(base))
	copy(original, base)

	BuildEnv(base, "111111111111")

	assert.Equal(t, original, base)
}

func TestBuildEnvPartialOverride(t *testing.T) {
	base := []string{"AWS_ACCESS_KEY_ID=custom-key"}
	env := BuildEnv(base, "")

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=custom-key")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
}

// An account overrides an ambient access key outright: LocalStack derives the
// account from it, so a set-if-absent default would silently ignore --account.
func TestBuildEnvAccountOverridesAmbientKey(t *testing.T) {
	base := []string{"AWS_ACCESS_KEY_ID=ambient-key"}
	env := BuildEnv(base, "111111111111")

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=111111111111")
	assert.NotContains(t, env, "AWS_ACCESS_KEY_ID=ambient-key")
}

func TestBuildEnvDropsSessionToken(t *testing.T) {
	base := []string{"AWS_SESSION_TOKEN=stale-token", "PATH=/usr/bin"}
	env := BuildEnv(base, "111111111111")

	assert.NotContains(t, env, "AWS_SESSION_TOKEN=stale-token")
	assert.Contains(t, env, "PATH=/usr/bin")
}

func TestExecEnvNoProfileSeedsDefaults(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	env := execEnv(base, ExecOptions{Account: "111111111111"})

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=111111111111")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
	assert.NotContains(t, env, "AWS_PROFILE=localstack")
}

// The profile is selected through AWS_PROFILE, never a --profile argument:
// an explicitly named profile removes botocore's environment credential
// provider, which would make the account below unreachable.
func TestExecEnvProfileWithSelectedAccount(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	env := execEnv(base, ExecOptions{
		Account:         "111111111111",
		UseProfile:      true,
		AccountSelected: true,
	})

	assert.Contains(t, env, "AWS_PROFILE=localstack")
	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=111111111111")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
}

// Seeding a region alongside AWS_PROFILE would override the profile's own
// `region`, since environment variables outrank config-file values.
func TestExecEnvProfileSeedsNoRegion(t *testing.T) {
	base := []string{"PATH=/usr/bin"}

	selected := execEnv(base, ExecOptions{Account: "111111111111", UseProfile: true, AccountSelected: true})
	assert.NotContains(t, selected, "AWS_DEFAULT_REGION=us-east-1")

	unselected := execEnv(base, ExecOptions{Account: "test", UseProfile: true})
	assert.NotContains(t, unselected, "AWS_DEFAULT_REGION=us-east-1")
}

func TestExecEnvProfileWithoutAccountRemovesCredentials(t *testing.T) {
	base := []string{
		"AWS_ACCESS_KEY_ID=ambient-key",
		"AWS_SECRET_ACCESS_KEY=ambient-secret",
		"AWS_SESSION_TOKEN=ambient-token",
		"PATH=/usr/bin",
	}
	env := execEnv(base, ExecOptions{Account: "test", UseProfile: true})

	assert.Contains(t, env, "AWS_PROFILE=localstack")
	for _, e := range env {
		assert.NotContains(t, []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"},
			keyOf(e), "credential variable must be removed entirely, got %q", e)
	}
	assert.Contains(t, env, "PATH=/usr/bin")
}

// A lone ambient access key would make the AWS CLI fail with "Partial
// credentials found in env" once the environment provider is back in the chain.
func TestExecEnvProfileNeverLeavesPartialCredentials(t *testing.T) {
	base := []string{"AWS_ACCESS_KEY_ID=111111111111"}

	selected := execEnv(base, ExecOptions{Account: "111111111111", UseProfile: true, AccountSelected: true})
	assert.True(t, hasKey(selected, "AWS_ACCESS_KEY_ID") && hasKey(selected, "AWS_SECRET_ACCESS_KEY"),
		"selected account must supply both halves of the pair")

	unselected := execEnv(base, ExecOptions{Account: "111111111111", UseProfile: true})
	assert.False(t, hasKey(unselected, "AWS_ACCESS_KEY_ID") || hasKey(unselected, "AWS_SECRET_ACCESS_KEY"),
		"without a selection neither half may be present")
}

func TestExecEnvProfileRemovesDeprecatedProfileVar(t *testing.T) {
	base := []string{"AWS_DEFAULT_PROFILE=production"}
	env := execEnv(base, ExecOptions{Account: "test", UseProfile: true})

	assert.Contains(t, env, "AWS_PROFILE=localstack")
	assert.False(t, hasKey(env, "AWS_DEFAULT_PROFILE"))
}

func TestExecEnvForcesUnbufferedPython(t *testing.T) {
	base := []string{"PATH=/usr/bin"}

	withProfile := execEnv(base, ExecOptions{Account: "test", UseProfile: true})
	assert.Contains(t, withProfile, "PYTHONUNBUFFERED=1")
	assert.NotContains(t, withProfile, "AWS_ACCESS_KEY_ID=test")

	withoutProfile := execEnv(base, ExecOptions{Account: "test"})
	assert.Contains(t, withoutProfile, "PYTHONUNBUFFERED=1")
	assert.Contains(t, withoutProfile, "AWS_ACCESS_KEY_ID=test")
}

func TestExecEnvPreservesUserPythonUnbuffered(t *testing.T) {
	base := []string{"PYTHONUNBUFFERED=x"}
	env := execEnv(base, ExecOptions{Account: "test", UseProfile: true})

	assert.Contains(t, env, "PYTHONUNBUFFERED=x")
	assert.NotContains(t, env, "PYTHONUNBUFFERED=1")
}

func TestExecEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AWS_ACCESS_KEY_ID=ambient", "AWS_SESSION_TOKEN=tok"}
	original := make([]string, len(base))
	copy(original, base)

	execEnv(base, ExecOptions{Account: "test", UseProfile: true})
	execEnv(base, ExecOptions{Account: "111111111111", UseProfile: true, AccountSelected: true})
	execEnv(base, ExecOptions{Account: "111111111111"})

	assert.Equal(t, original, base)
}

func keyOf(entry string) string {
	for i := 0; i < len(entry); i++ {
		if entry[i] == '=' {
			return entry[:i]
		}
	}
	return entry
}

func hasKey(env []string, key string) bool {
	for _, e := range env {
		if keyOf(e) == key {
			return true
		}
	}
	return false
}
