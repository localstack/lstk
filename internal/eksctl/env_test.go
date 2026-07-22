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

	for _, k := range endpointEnvVars {
		assert.Equalf(t, url, env[k], "expected %s to point at LocalStack", k)
	}
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

func TestBuildEnvRespectsUserRegionAndAccount(t *testing.T) {
	base := []string{"AWS_REGION=eu-west-1", "AWS_ACCESS_KEY_ID=111111111111"}
	env := envMap(BuildEnv(base, "http://localhost.localstack.cloud:4566"))

	assert.Equal(t, "eu-west-1", env["AWS_REGION"])
	assert.Equal(t, "111111111111", env["AWS_ACCESS_KEY_ID"])
	// AWS_DEFAULT_REGION is still defaulted since only AWS_REGION was set.
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

	for _, k := range endpointEnvVars {
		_, ok := env[k]
		assert.Falsef(t, ok, "%s must not be set when endpointURL is empty", k)
	}
	// Credential defaults are still applied.
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
}

func TestBuildEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin", "AWS_PROFILE=real"}
	original := append([]string(nil), base...)

	BuildEnv(base, "http://localhost.localstack.cloud:4566")

	assert.Equal(t, original, base)
}
