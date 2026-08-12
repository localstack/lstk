package integration_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests do not exercise lstk. They pin the botocore behavior lstk's
// credential handling is built on, against whatever real AWS CLI is installed.
//
// `lstk aws` selects the localstack profile with AWS_PROFILE rather than
// --profile because the two resolve credentials differently: botocore removes
// the environment credential provider from its chain when a profile arrives as
// an explicit instance variable, which --profile sets and AWS_PROFILE does not.
// Everything in internal/awscli's execEnv follows from that. A fake `aws` script
// cannot detect a regression here, and the behavior is an implementation detail
// of a dependency lstk does not pin — so it is asserted directly.
//
// Measured on aws-cli/2.35.15. AWS CLI v1 shares the same botocore logic, which
// has been in place since well before v2 existed, but is not covered here: v1 is
// pip-installed and rarely present alongside v2. If a regression ever surfaces
// on v1, this is the test to extend.

// awsProfileFixture writes a config/credentials pair whose profile carries a
// non-default region, and returns the environment overrides that select them.
func awsProfileFixture(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()

	configPath := filepath.Join(dir, "config")
	require.NoError(t, os.WriteFile(configPath, []byte(
		"[profile localstack]\nregion = eu-west-1\noutput = table\n"), 0600))

	credsPath := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(credsPath, []byte(
		"[localstack]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0600))

	return []string{
		"AWS_CONFIG_FILE=" + configPath,
		"AWS_SHARED_CREDENTIALS_FILE=" + credsPath,
	}
}

// awsProbeEnv returns the environment for a probe: the caller's own, with every
// AWS_* variable removed, plus the given overrides.
//
// Stripping rather than building a minimal environment from scratch is what
// makes this portable. The assertions only require that nothing but the fixture
// supplies credentials, a region, or a profile, which removing AWS_* guarantees;
// meanwhile the frozen AWS CLI needs the platform's own variables to start at
// all. A hand-built PATH+HOME environment aborted the Windows CLI during import
// with "Could not determine home directory", because Python resolves the home
// directory from USERPROFILE there, not HOME.
func awsProbeEnv(extra ...string) []string {
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "AWS_") {
			continue
		}
		env = append(env, e)
	}
	return append(env, extra...)
}

// awsConfigureList runs the real `aws configure list` with the given extra
// environment, skipping the test when no aws binary is installed.
func awsConfigureList(t *testing.T, extraEnv ...string) string {
	t.Helper()
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}

	cmd := exec.CommandContext(testContext(t), "aws", "configure", "list")
	cmd.Env = awsProbeEnv(extraEnv...)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "aws configure list failed: %s", out)
	return string(out)
}

// AWS_PROFILE leaves the environment credential provider in the chain, so
// credentials come from the environment while the profile still supplies the
// region. This is what lets `lstk aws --account …` work without dropping the
// profile.
func TestAWSProfileEnvVarLetsEnvironmentCredentialsWin(t *testing.T) {
	t.Parallel()

	e := append(awsProfileFixture(t),
		"AWS_PROFILE=localstack",
		"AWS_ACCESS_KEY_ID=111111111111",
		"AWS_SECRET_ACCESS_KEY=test",
	)
	out := awsConfigureList(t, e...)

	require.Regexp(t, `access_key\s*:\s*\**1111\s*:\s*env`, out,
		"credentials must come from the environment under AWS_PROFILE")
	require.Regexp(t, `region\s*:\s*eu-west-1\s*:\s*config-file`, out,
		"the profile must still supply the region")
}

// The contrast case, and the reason lstk stopped passing --profile: an
// explicitly named profile wins over environment credentials, which would make
// account selection silently impossible.
func TestAWSProfileFlagSuppressesEnvironmentCredentials(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}

	cmd := exec.CommandContext(testContext(t), "aws", "configure", "list", "--profile", "localstack")
	cmd.Env = awsProbeEnv(append(awsProfileFixture(t),
		"AWS_ACCESS_KEY_ID=111111111111",
		"AWS_SECRET_ACCESS_KEY=test",
	)...)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "aws configure list failed: %s", out)

	require.Regexp(t, `access_key\s*:\s*\**test\s*:\s*shared-credentials-file`, string(out),
		"--profile must suppress the environment credentials (the behavior lstk avoids)")
}

// Seeding a region alongside AWS_PROFILE would override the profile's own,
// which is why lstk seeds no defaults while a profile is in use.
func TestAWSEnvironmentRegionOverridesProfileRegion(t *testing.T) {
	t.Parallel()

	e := append(awsProfileFixture(t),
		"AWS_PROFILE=localstack",
		"AWS_ACCESS_KEY_ID=111111111111",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_DEFAULT_REGION=us-east-1",
	)
	out := awsConfigureList(t, e...)

	require.Regexp(t, `region\s*:\s*us-east-1\s*:\s*env`, out,
		"an environment region outranks the profile's, so lstk must not seed one")
}

// AWS_PROFILE and AWS_DEFAULT_PROFILE are resolved from one ordered list whose
// order has varied across botocore versions. lstk removes the deprecated
// spelling rather than depending on which wins; this records the behavior seen.
func TestAWSProfileOutranksDefaultProfile(t *testing.T) {
	t.Parallel()

	e := append(awsProfileFixture(t),
		"AWS_PROFILE=localstack",
		"AWS_DEFAULT_PROFILE=does-not-exist",
	)
	out := awsConfigureList(t, e...)

	require.Regexp(t, `region\s*:\s*eu-west-1\s*:\s*config-file`, out)
}
