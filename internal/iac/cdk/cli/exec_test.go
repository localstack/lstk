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
	env := envMap(BuildEnv(nil, "http://localhost.localstack.cloud:4566", "http://s3.localhost.localstack.cloud:4566", "eu-west-1", "123456789012"))

	assert.Equal(t, "http://localhost.localstack.cloud:4566", env["AWS_ENDPOINT_URL"])
	assert.Equal(t, "http://s3.localhost.localstack.cloud:4566", env["AWS_ENDPOINT_URL_S3"])
	assert.Equal(t, "123456789012", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])
	assert.Equal(t, "eu-west-1", env["AWS_REGION"])
	assert.Equal(t, "eu-west-1", env["AWS_DEFAULT_REGION"])
	assert.Equal(t, "1", env["CDK_DISABLE_LEGACY_EXPORT_WARNING"])
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
	env := envMap(BuildEnv(base, "http://127.0.0.1:4566", "http://127.0.0.1:4566", "us-east-1", "test"))

	_, hasProfile := env["AWS_PROFILE"]
	_, hasDefaultProfile := env["AWS_DEFAULT_PROFILE"]
	_, hasSessionToken := env["AWS_SESSION_TOKEN"]
	assert.False(t, hasProfile, "AWS_PROFILE must be stripped")
	assert.False(t, hasDefaultProfile, "AWS_DEFAULT_PROFILE must be stripped")
	assert.False(t, hasSessionToken, "AWS_SESSION_TOKEN must be stripped")

	// Real creds in base are overridden with mock values, not preserved.
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
	assert.Equal(t, "test", env["AWS_SECRET_ACCESS_KEY"])

	// Unrelated entries are preserved.
	assert.Equal(t, "/usr/bin", env["PATH"])
	assert.Equal(t, "/home/user", env["HOME"])
}

func TestBuildEnvSkipsEmptyEndpoint(t *testing.T) {
	env := envMap(BuildEnv(nil, "", "", "us-east-1", "test"))
	_, hasEndpoint := env["AWS_ENDPOINT_URL"]
	_, hasS3 := env["AWS_ENDPOINT_URL_S3"]
	assert.False(t, hasEndpoint, "empty AWS_ENDPOINT_URL must not be set")
	assert.False(t, hasS3, "empty AWS_ENDPOINT_URL_S3 must not be set")
	// Region/creds are still set even with no endpoint.
	assert.Equal(t, "us-east-1", env["AWS_REGION"])
	assert.Equal(t, "test", env["AWS_ACCESS_KEY_ID"])
}

func TestIsOffline(t *testing.T) {
	offline := [][]string{
		{"synth"},
		{"--app", "foo", "ls"},
		{"init", "--language", "typescript"},
		{"doctor"},
		{"acknowledge", "12345"},
		{"context"},
	}
	for _, args := range offline {
		assert.Truef(t, IsOffline(args), "expected %v offline", args)
	}

	awsContacting := [][]string{
		{"deploy", "MyStack"},
		{"bootstrap"},
		{"destroy", "--force"},
		{"diff"},
		{"watch"},
		{}, // no subcommand → not offline (gate on emulator)
	}
	for _, args := range awsContacting {
		assert.Falsef(t, IsOffline(args), "expected %v not offline", args)
	}
}
