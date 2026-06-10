package cli

import (
	"os"
	"strings"
)

// Environment variables this package reads. They are process environment, not
// lstk config, so reading them here (the domain boundary for terraform) is
// consistent with the rule that domain code must not call config.Get().

// tfCmd returns the terraform binary name to invoke, honoring LSTK_TF_CMD (e.g.
// "tofu") and defaulting to "terraform".
func tfCmd() string {
	if v := os.Getenv("LSTK_TF_CMD"); v != "" {
		return v
	}
	return "terraform"
}

// overrideFileName returns the name of the provider-override file to generate,
// honoring LSTK_TF_OVERRIDE_FILE_NAME and defaulting to
// localstack_providers_override.tf.
func overrideFileName() string {
	if v := os.Getenv("LSTK_TF_OVERRIDE_FILE_NAME"); v != "" {
		return v
	}
	return defaultOverrideFileName
}

// dryRun reports whether LSTK_TF_DRY_RUN is enabled. When set, lstk generates
// the override file but does not invoke terraform.
func dryRun() bool {
	return isTruthy(os.Getenv("LSTK_TF_DRY_RUN"))
}

// endpointURLOverride returns the value of AWS_ENDPOINT_URL, which takes
// precedence over the auto-resolved LocalStack endpoint when building the
// override.
func endpointURLOverride() string {
	return os.Getenv("AWS_ENDPOINT_URL")
}

func isTruthy(v string) bool {
	switch strings.ToLower(v) {
	case "", "0", "false", "no", "off":
		return false
	default:
		return true
	}
}
