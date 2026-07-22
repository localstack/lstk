package eksctl

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// envMap parses an env slice ("K=V") into a map for assertions.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			m[k] = v
		}
	}
	return m
}

func TestBuildEnvSetsAllServiceEndpoints(t *testing.T) {
	const url = "http://localhost.localstack.cloud:4566"
	env := envMap(BuildEnv(nil, url))

	for _, k := range endpointEnvVars() {
		assert.Equalf(t, url, env[k], "expected %s to point at LocalStack", k)
	}
	assert.Equal(t, "false", env["AWS_IGNORE_CONFIGURED_ENDPOINT_URLS"])
	// Credential and region defaults are filled in.
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])
	assert.Equal(t, "us-east-1", env["AWS_REGION"])
	assert.Equal(t, "us-east-1", env["AWS_DEFAULT_REGION"])
}

func TestBuildEnvOverridesExistingEndpoints(t *testing.T) {
	const url = "http://localhost.localstack.cloud:4566"
	base := []string{"AWS_EKS_ENDPOINT=https://eks.eu-west-1.amazonaws.com"}
	env := envMap(BuildEnv(base, url))

	assert.Equal(t, url, env["AWS_EKS_ENDPOINT"], "a pre-existing endpoint must be overridden to LocalStack")
}

func TestBuildEnvRemovesHigherPrecedenceEndpointConfig(t *testing.T) {
	const url = "http://localhost.localstack.cloud:4566"
	base := []string{
		"AWS_ENDPOINT_URL_SSM=https://ssm.us-east-1.amazonaws.com",
		"AWS_CLOUDTRAIL_ENDPOINT=https://cloudtrail.us-east-1.amazonaws.com",
		"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=true",
	}
	env := envMap(BuildEnv(base, url))

	_, hasSSMEndpoint := env["AWS_ENDPOINT_URL_SSM"]
	_, hasCloudTrailEndpoint := env["AWS_CLOUDTRAIL_ENDPOINT"]
	assert.False(t, hasSSMEndpoint)
	assert.False(t, hasCloudTrailEndpoint)
	assert.Equal(t, "false", env["AWS_IGNORE_CONFIGURED_ENDPOINT_URLS"])
	assert.Equal(t, url, env["AWS_ENDPOINT_URL"])
}

func TestBuildEnvRespectsUserRegionAndAccount(t *testing.T) {
	base := []string{"AWS_REGION=eu-west-1", "AWS_ACCESS_KEY_ID=111111111111"}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	assert.Equal(t, "eu-west-1", env["AWS_REGION"])
	assert.Equal(t, "111111111111", env["AWS_ACCESS_KEY_ID"])
	// AWS_DEFAULT_REGION follows the user's region rather than the us-east-1
	// default, so the injected pair can never contradict the user's setting.
	assert.Equal(t, "eu-west-1", env["AWS_DEFAULT_REGION"])
}

func TestBuildEnvDefaultRegionOnlySeedsAWSRegion(t *testing.T) {
	// A user with only AWS_DEFAULT_REGION set must not have it shadowed by an
	// injected AWS_REGION=us-east-1 (the SDK resolves AWS_REGION first).
	base := []string{"AWS_DEFAULT_REGION=eu-central-1"}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	assert.Equal(t, "eu-central-1", env["AWS_REGION"])
	assert.Equal(t, "eu-central-1", env["AWS_DEFAULT_REGION"])
}

func TestBuildEnvKeepsContradictoryUserRegionsVerbatim(t *testing.T) {
	base := []string{"AWS_REGION=eu-west-1", "AWS_DEFAULT_REGION=us-west-2"}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	assert.Equal(t, "eu-west-1", env["AWS_REGION"])
	assert.Equal(t, "us-west-2", env["AWS_DEFAULT_REGION"])
}

func TestBuildEnvDefaultsEmptyCredentialsAndRegion(t *testing.T) {
	base := []string{
		"AWS_ACCESS_KEY_ID=",
		"AWS_SECRET_ACCESS_KEY=",
		"AWS_REGION=",
		"AWS_DEFAULT_REGION=",
	}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])
	assert.Equal(t, "us-east-1", env["AWS_REGION"])
	assert.Equal(t, "us-east-1", env["AWS_DEFAULT_REGION"])
}

func TestBuildEnvStripsAmbientAWSConfig(t *testing.T) {
	base := []string{
		"AWS_PROFILE=my-real-profile",
		"AWS_DEFAULT_PROFILE=other",
		"AWS_SESSION_TOKEN=realtoken",
		"PATH=/usr/bin",
	}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	_, hasProfile := env["AWS_PROFILE"]
	_, hasDefaultProfile := env["AWS_DEFAULT_PROFILE"]
	_, hasSessionToken := env["AWS_SESSION_TOKEN"]
	assert.False(t, hasProfile)
	assert.False(t, hasDefaultProfile)
	assert.False(t, hasSessionToken)
	// Unrelated variables are preserved.
	assert.Equal(t, "/usr/bin", env["PATH"])
}

func TestBuildEnvOfflineLeavesEndpointsUnset(t *testing.T) {
	env := envMap(BuildEnv(nil, ""))

	for _, k := range endpointEnvVars() {
		_, ok := env[k]
		assert.Falsef(t, ok, "%s must not be set when endpointURL is empty", k)
	}
	// Credential defaults are still applied.
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
}

func TestBuildEnvOfflineStillStripsAmbientConfig(t *testing.T) {
	base := []string{"AWS_PROFILE=my-real-profile", "AWS_SESSION_TOKEN=realtoken"}
	env := envMap(BuildEnv(base, ""))

	_, hasProfile := env["AWS_PROFILE"]
	_, hasSessionToken := env["AWS_SESSION_TOKEN"]
	assert.False(t, hasProfile)
	assert.False(t, hasSessionToken)
}

func TestBuildEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AWS_PROFILE=real"}
	original := append([]string(nil), base...)

	BuildEnv(base, "http://localhost.localstack.cloud:4566")

	assert.Equal(t, original, base)
}
