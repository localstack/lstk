package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func defaultOpts() Options {
	return resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://localhost:4566"},
			{Service: "S3", URL: "http://s3.localhost.localstack.cloud:4566"},
		},
	})
}

func TestBuildEnvSetsDefaultsWhenAbsent(t *testing.T) {
	env := buildEnv([]string{"PATH=/usr/bin"}, defaultOpts())

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=test")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=test")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=us-east-1")
	assert.Contains(t, env, "AWS_ENDPOINT_URL=http://localhost:4566")
	assert.Contains(t, env, "AWS_ENDPOINT_URL_S3=http://s3.localhost.localstack.cloud:4566")
	assert.Contains(t, env, "PATH=/usr/bin")
}

func TestBuildEnvPreservesNonEndpointBaseValues(t *testing.T) {
	// Non-endpoint AWS env vars in the parent shell follow setIfAbsent —
	// customer values win.
	base := []string{
		"AWS_ACCESS_KEY_ID=custom",
		"AWS_SECRET_ACCESS_KEY=secret",
		"AWS_DEFAULT_REGION=eu-west-1",
	}
	env := buildEnv(base, defaultOpts())

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=custom")
	assert.Contains(t, env, "AWS_SECRET_ACCESS_KEY=secret")
	assert.Contains(t, env, "AWS_DEFAULT_REGION=eu-west-1")
}

func TestBuildEnvStripsBaseEndpointEnvVars(t *testing.T) {
	// Endpoint env vars in the parent shell must be stripped — Options.Endpoints
	// (assembled at the cmd boundary) is the single source of truth.
	base := []string{
		"PATH=/usr/bin",
		"AWS_ENDPOINT_URL=http://stale:1111",
		"AWS_ENDPOINT_URL_S3=http://stale-s3:1111",
		"AWS_ENDPOINT_URL_LAMBDA=http://stale-lambda:1111",
	}
	opts := resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://lstk:4566"},
			{Service: "S3", URL: "http://lstk-s3:4566"},
		},
	})
	env := buildEnv(base, opts)

	assert.Contains(t, env, "PATH=/usr/bin")
	assert.Contains(t, env, "AWS_ENDPOINT_URL=http://lstk:4566")
	assert.Contains(t, env, "AWS_ENDPOINT_URL_S3=http://lstk-s3:4566")
	for _, e := range env {
		if strings.HasPrefix(e, "AWS_ENDPOINT_URL=http://stale") ||
			strings.HasPrefix(e, "AWS_ENDPOINT_URL_S3=http://stale") {
			t.Fatalf("buildEnv left a stale endpoint env var; env: %v", env)
		}
		if e == "AWS_ENDPOINT_URL_LAMBDA=http://stale-lambda:1111" {
			t.Fatalf("buildEnv leaked stale AWS_ENDPOINT_URL_LAMBDA; env: %v", env)
		}
	}
}

func TestBuildEnvSkipsEmptyEndpoints(t *testing.T) {
	opts := resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://localhost:4566"},
			{Service: "S3", URL: ""}, // empty: must not produce AWS_ENDPOINT_URL_S3=
		},
	})
	env := buildEnv(nil, opts)

	for _, e := range env {
		if e == "AWS_ENDPOINT_URL_S3=" {
			t.Fatalf("buildEnv emitted empty AWS_ENDPOINT_URL_S3=; env: %v", env)
		}
	}
	// Sanity: default endpoint did make it in.
	assert.Contains(t, env, "AWS_ENDPOINT_URL=http://localhost:4566")
}

func TestBuildEnvEmitsArbitraryEndpointServicesFromOptions(t *testing.T) {
	// Once the cmd layer has added a service to Options.Endpoints, buildEnv
	// must emit it as AWS_ENDPOINT_URL_<SERVICE>.
	opts := resolveOptionsWithDefaults(Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://lstk:4566"},
			{Service: "S3", URL: "http://lstk-s3:4566"},
			{Service: "LAMBDA", URL: "http://customer-lambda:9000"},
			{Service: "DYNAMODB", URL: "http://customer-ddb:9000"},
		},
	})
	env := buildEnv(nil, opts)

	assert.Contains(t, env, "AWS_ENDPOINT_URL_LAMBDA=http://customer-lambda:9000")
	assert.Contains(t, env, "AWS_ENDPOINT_URL_DYNAMODB=http://customer-ddb:9000")
}

func TestBuildEnvDoesNotMutateInput(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	original := append([]string{}, base...)

	buildEnv(base, defaultOpts())

	assert.Equal(t, original, base)
}

func TestEndpointFor(t *testing.T) {
	opts := Options{
		Endpoints: []Endpoint{
			{Service: "", URL: "http://default:4566"},
			{Service: "S3", URL: "http://s3:4566"},
		},
	}
	assert.Equal(t, "http://s3:4566", opts.endpointFor("S3"), "exact match wins")
	assert.Equal(t, "http://default:4566", opts.endpointFor("DYNAMODB"), "unknown service falls back to default")
	assert.Equal(t, "http://default:4566", opts.endpointFor(""), "default endpoint lookup")
}

func TestEndpointForNoDefault(t *testing.T) {
	opts := Options{
		Endpoints: []Endpoint{{Service: "S3", URL: "http://s3:4566"}},
	}
	assert.Equal(t, "", opts.endpointFor("DYNAMODB"), "no default → empty")
	assert.Equal(t, "http://s3:4566", opts.endpointFor("S3"))
}

func TestExtractChdir(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no chdir", []string{"plan"}, ""},
		{"present", []string{"-chdir=infra", "plan"}, "infra"},
		{"absolute", []string{"-chdir=/tmp/x", "apply"}, "/tmp/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, extractChdir(tc.args))
		})
	}
}

func TestResolveOptionsCustomizeAccessKey(t *testing.T) {
	got := resolveOptionsWithDefaults(Options{AccessKey: "AKIAEXAMPLE", CustomizeAccessKey: true})
	assert.Equal(t, "LKIAEXAMPLE", got.AccessKey)
	// Idempotent under re-resolution.
	got2 := resolveOptionsWithDefaults(got)
	assert.Equal(t, "LKIAEXAMPLE", got2.AccessKey)
}

func TestBuildEnvCustomizeAccessKeyOverridesParentEnv(t *testing.T) {
	// The whole point of CUSTOMIZE_ACCESS_KEY is to neutralise an
	// accidentally-real AWS key set in the parent shell, so setIfAbsent
	// semantics must NOT apply here.
	opts := resolveOptionsWithDefaults(Options{
		AccessKey:          "AKIAEXAMPLE",
		CustomizeAccessKey: true,
	})
	env := buildEnv([]string{"AWS_ACCESS_KEY_ID=AKIAEXAMPLE"}, opts)

	assert.Contains(t, env, "AWS_ACCESS_KEY_ID=LKIAEXAMPLE")
	for _, e := range env {
		if e == "AWS_ACCESS_KEY_ID=AKIAEXAMPLE" {
			t.Fatalf("buildEnv left the original AKIA key in place; env: %v", env)
		}
	}
}
