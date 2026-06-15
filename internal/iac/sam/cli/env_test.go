package cli

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

func TestBuildEnvSetsLocalStackValues(t *testing.T) {
	env := envMap(BuildEnv(nil, "http://localhost.localstack.cloud:4566", "111111111111", "eu-west-1"))

	assert.Equal(t, "http://localhost.localstack.cloud:4566", env["AWS_ENDPOINT_URL"])
	assert.Equal(t, "111111111111", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])
	// SAM reads AWS_DEFAULT_REGION, not AWS_REGION; lstk sets both.
	assert.Equal(t, "eu-west-1", env["AWS_REGION"])
	assert.Equal(t, "eu-west-1", env["AWS_DEFAULT_REGION"])

	// lstk leaves the user's SAM telemetry preference untouched.
	_, hasTelemetry := env["SAM_CLI_TELEMETRY"]
	assert.False(t, hasTelemetry, "SAM_CLI_TELEMETRY must not be set by lstk")

	// Unlike cdk, lstk never sets an S3-specific endpoint.
	_, hasS3 := env["AWS_ENDPOINT_URL_S3"]
	assert.False(t, hasS3, "AWS_ENDPOINT_URL_S3 must not be set by lstk")
}

// The resolved account is written to AWS_ACCESS_KEY_ID, overriding any ambient
// value, so SAM (and LocalStack) use the account lstk decided on.
func TestBuildEnvWritesResolvedAccount(t *testing.T) {
	base := []string{"AWS_ACCESS_KEY_ID=123456789012", "AWS_SECRET_ACCESS_KEY=somesecret"}
	env := envMap(BuildEnv(base, "http://127.0.0.1:4566", "test", "us-east-1"))

	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])
}

func TestBuildEnvStripsAmbientAWSConfig(t *testing.T) {
	base := []string{
		"AWS_PROFILE=my-real-profile",
		"AWS_DEFAULT_PROFILE=other",
		"AWS_SESSION_TOKEN=realtoken",
		"AWS_ACCESS_KEY_ID=AKIAREALKEY",
		"AWS_SECRET_ACCESS_KEY=realsecret",
		"PATH=/usr/bin",
		"HOME=/home/user",
	}
	env := envMap(BuildEnv(base, "http://127.0.0.1:4566", "test", "us-east-1"))

	_, hasProfile := env["AWS_PROFILE"]
	_, hasDefaultProfile := env["AWS_DEFAULT_PROFILE"]
	_, hasSessionToken := env["AWS_SESSION_TOKEN"]
	assert.False(t, hasProfile, "AWS_PROFILE must be stripped")
	assert.False(t, hasDefaultProfile, "AWS_DEFAULT_PROFILE must be stripped")
	assert.False(t, hasSessionToken, "AWS_SESSION_TOKEN must be stripped")

	// Real creds in base are overridden with the resolved values, not preserved.
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])

	// Unrelated entries are preserved.
	assert.Equal(t, "/usr/bin", env["PATH"])
	assert.Equal(t, "/home/user", env["HOME"])
}

// A user-set AWS_ENDPOINT_URL_S3 is neither set nor stripped by lstk; it passes
// through untouched as an escape hatch for exotic S3 addressing cases.
func TestBuildEnvPassesThroughUserS3Endpoint(t *testing.T) {
	base := []string{"AWS_ENDPOINT_URL_S3=http://s3.example.test:4566"}
	env := envMap(BuildEnv(base, "http://127.0.0.1:4566", "test", "us-east-1"))

	assert.Equal(t, "http://s3.example.test:4566", env["AWS_ENDPOINT_URL_S3"])
}

func TestBuildEnvSkipsEmptyEndpoint(t *testing.T) {
	env := envMap(BuildEnv(nil, "", "test", "us-east-1"))
	_, hasEndpoint := env["AWS_ENDPOINT_URL"]
	assert.False(t, hasEndpoint, "empty AWS_ENDPOINT_URL must not be set")
	// Region/creds are still set even with no endpoint.
	assert.Equal(t, "us-east-1", env["AWS_REGION"])
	assert.Equal(t, "us-east-1", env["AWS_DEFAULT_REGION"])
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
}
