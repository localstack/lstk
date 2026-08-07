package eksctl

import (
	"os"
	"strings"
)

// endpointEnvVars returns the AWS service endpoint variables set to the
// resolved LocalStack endpoint. The seven AWS_<SVC>_ENDPOINT names are the ones
// eksctl resolves itself per client; AWS_ENDPOINT_URL covers clients eksctl
// builds without one of those overrides, including SSM and Outposts.
func endpointEnvVars() []string {
	return []string{
		"AWS_CLOUDFORMATION_ENDPOINT",
		"AWS_EC2_ENDPOINT",
		"AWS_EKS_ENDPOINT",
		"AWS_ELB_ENDPOINT",
		"AWS_ELBV2_ENDPOINT",
		"AWS_IAM_ENDPOINT",
		"AWS_STS_ENDPOINT",
		"AWS_ENDPOINT_URL",
	}
}

// endpointURLOverride returns AWS_ENDPOINT_URL from the process environment,
// which takes precedence over the auto-resolved LocalStack endpoint (same
// contract as the terraform/cdk/sam proxies).
func endpointURLOverride() string {
	return os.Getenv("AWS_ENDPOINT_URL")
}

func isStrippedAWSConfigKey(key string) bool {
	switch key {
	case "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_SESSION_TOKEN":
		return true
	default:
		return false
	}
}

// isEndpointConfigKey reports whether key can override the LocalStack endpoint
// for an AWS service. Service-specific AWS_ENDPOINT_URL_<SERVICE> values take
// precedence over AWS_ENDPOINT_URL, while eksctl also supports legacy
// AWS_<SERVICE>_ENDPOINT variables for some clients.
func isEndpointConfigKey(key string) bool {
	return key == "AWS_ENDPOINT_URL" ||
		key == "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS" ||
		strings.HasPrefix(key, "AWS_ENDPOINT_URL_") ||
		(strings.HasPrefix(key, "AWS_") && strings.HasSuffix(key, "_ENDPOINT"))
}

// BuildEnv returns the environment for the eksctl subprocess: base with ambient
// AWS profile/session config stripped, the LocalStack service endpoint variables
// set (overriding any pre-existing entries), and credential/region defaults
// filled in when absent or empty so a meaningful user-provided AWS_REGION or
// AWS_ACCESS_KEY_ID is respected.
//
// When endpointURL is empty (offline subcommands like `version`/`completion`),
// no endpoint variables are set or stripped — the invocation does not contact
// LocalStack. The profile/session keys are still stripped and the credential
// defaults still applied, keeping the subprocess environment predictable on
// every path.
func BuildEnv(base []string, endpointURL string) []string {
	endpointKeys := endpointEnvVars()

	env := make([]string, 0, len(base)+len(endpointKeys)+5)
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			env = append(env, e)
			continue
		}
		if isStrippedAWSConfigKey(key) || (endpointURL != "" && isEndpointConfigKey(key)) {
			continue
		}
		env = append(env, e)
	}

	if endpointURL != "" {
		for _, k := range endpointKeys {
			env = append(env, k+"="+endpointURL)
		}
		// A caller or AWS profile can otherwise make SDK-managed clients ignore
		// the generic AWS_ENDPOINT_URL and fall back to their real endpoints.
		env = append(env, "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS=false")
	}

	// LocalStack derives the account id from the access key; "test" maps to the
	// default account. Region defaults to us-east-1. Both are only defaults —
	// a user-set value (or eksctl's own --region flag) takes precedence.
	setDefault(&env, "AWS_ACCESS_KEY_ID", "test")
	setDefault(&env, "AWS_SECRET_ACCESS_KEY", "test")

	// Resolve one region and default both variables to it. Defaulting them
	// independently would let an injected AWS_REGION=us-east-1 shadow a
	// user-set AWS_DEFAULT_REGION (the SDK resolves AWS_REGION first), moving
	// the cluster to a region the user never asked for.
	region := lookup(env, "AWS_REGION")
	if region == "" {
		region = lookup(env, "AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	setDefault(&env, "AWS_REGION", region)
	setDefault(&env, "AWS_DEFAULT_REGION", region)

	return env
}

func lookup(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

func setDefault(env *[]string, key, value string) {
	prefix := key + "="
	for i, e := range *env {
		if strings.HasPrefix(e, prefix) {
			if len(e) == len(prefix) {
				(*env)[i] = prefix + value
			}
			return
		}
	}
	*env = append(*env, prefix+value)
}
