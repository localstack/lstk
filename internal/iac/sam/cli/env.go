package cli

import (
	"os"
	"strings"
)

// Environment variables this package reads. They are process environment, not
// lstk config, so reading them here (the domain boundary for sam) is consistent
// with the rule that domain code must not call config.Get().

// samCmd returns the SAM CLI binary name to invoke, honoring LSTK_SAM_CMD and
// defaulting to "sam".
func samCmd() string {
	if v := os.Getenv("LSTK_SAM_CMD"); v != "" {
		return v
	}
	return "sam"
}

// endpointURLOverride returns AWS_ENDPOINT_URL, which takes precedence over the
// auto-resolved LocalStack endpoint.
func endpointURLOverride() string {
	return os.Getenv("AWS_ENDPOINT_URL")
}

// strippedKeys are ambient AWS configuration variables removed from the SAM
// subprocess environment. A named profile, default profile, or stale session
// token could otherwise resolve real credentials/region and silently redirect a
// deploy at real AWS. (samlocal itself does not strip these — it relies on its
// in-process boto3 endpoint patch — but lstk has no such patch, so it isolates
// the environment instead.)
var strippedKeys = map[string]bool{
	"AWS_PROFILE":         true,
	"AWS_DEFAULT_PROFILE": true,
	"AWS_SESSION_TOKEN":   true,
}

// BuildEnv returns the environment for the sam subprocess: base with ambient AWS
// configuration stripped and the LocalStack-pointing values set (overriding any
// pre-existing entries). Empty endpoint values are not set, so they never
// clobber a meaningful inherited value with "".
//
// account is written to AWS_ACCESS_KEY_ID: SAM passes it through and LocalStack
// derives the account id from it (the Terraform model). The region is written to
// both AWS_REGION and AWS_DEFAULT_REGION because SAM reads AWS_DEFAULT_REGION
// (not AWS_REGION); setting both is harmless and overwrites any stale ambient
// AWS_REGION.
//
// Unlike the CDK proxy, no S3-specific endpoint is set: SAM's botocore
// auto-selects path-style addressing against a localhost/IP endpoint, so plain
// AWS_ENDPOINT_URL suffices. AWS_ENDPOINT_URL_S3 is deliberately neither set nor
// stripped — a user who needs to override S3 addressing for an exotic case can
// set it and botocore honors it.
func BuildEnv(base []string, endpointURL, account, region string) []string {
	// Ordered so the produced environment is deterministic. Empty-valued
	// entries are skipped below.
	managed := []struct{ key, value string }{
		{"AWS_ENDPOINT_URL", endpointURL},
		{"AWS_ACCESS_KEY_ID", account},
		{"AWS_SECRET_ACCESS_KEY", "test"},
		{"AWS_REGION", region},
		{"AWS_DEFAULT_REGION", region},
	}

	managedKeys := make(map[string]bool, len(managed))
	for _, m := range managed {
		managedKeys[m.key] = true
	}

	env := make([]string, 0, len(base)+len(managed))
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			env = append(env, e)
			continue
		}
		if strippedKeys[key] || managedKeys[key] {
			continue
		}
		env = append(env, e)
	}
	for _, m := range managed {
		if m.value == "" {
			continue
		}
		env = append(env, m.key+"="+m.value)
	}
	return env
}
