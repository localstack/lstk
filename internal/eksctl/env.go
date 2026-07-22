package eksctl

import "strings"

// endpointEnvVars are the AWS service endpoint variables eksctl (via aws-sdk-go)
// reads to route each service at a custom endpoint. lstk sets them all to the
// resolved LocalStack endpoint so `eksctl create cluster` and friends target the
// emulator. This is the "Newer Versions" flow from the LocalStack eksctl docs;
// older eksctl releases ignore these variables (see version.go for the gate).
var endpointEnvVars = []string{
	"AWS_CLOUDFORMATION_ENDPOINT",
	"AWS_EC2_ENDPOINT",
	"AWS_EKS_ENDPOINT",
	"AWS_ELB_ENDPOINT",
	"AWS_ELBV2_ENDPOINT",
	"AWS_IAM_ENDPOINT",
	"AWS_STS_ENDPOINT",
}

// strippedKeys are ambient AWS configuration variables removed from the eksctl
// subprocess environment. A named profile, default profile, or stale session
// token could otherwise resolve real credentials and silently redirect a
// cluster operation at real AWS. The endpoint variables above pin the service
// endpoints at LocalStack regardless, but stripping these keeps credentials and
// account resolution predictable.
var strippedKeys = map[string]bool{
	"AWS_PROFILE":         true,
	"AWS_DEFAULT_PROFILE": true,
	"AWS_SESSION_TOKEN":   true,
}

// BuildEnv returns the environment for the eksctl subprocess: base with ambient
// AWS profile/session config stripped, the LocalStack service endpoint variables
// set (overriding any pre-existing entries), and credential/region defaults
// filled in only when absent so a user-provided AWS_REGION or AWS_ACCESS_KEY_ID
// is respected.
//
// When endpointURL is empty (offline subcommands like `version`/`completion`),
// no endpoint variables are set and none are stripped — the invocation does not
// contact LocalStack, so the caller's environment is left as-is apart from the
// credential defaults.
func BuildEnv(base []string, endpointURL string) []string {
	managed := make(map[string]bool, len(endpointEnvVars))
	if endpointURL != "" {
		for _, k := range endpointEnvVars {
			managed[k] = true
		}
	}

	env := make([]string, 0, len(base)+len(endpointEnvVars)+4)
	for _, e := range base {
		key, _, ok := strings.Cut(e, "=")
		if !ok {
			env = append(env, e)
			continue
		}
		if strippedKeys[key] || managed[key] {
			continue
		}
		env = append(env, e)
	}

	if endpointURL != "" {
		for _, k := range endpointEnvVars {
			env = append(env, k+"="+endpointURL)
		}
	}

	// LocalStack derives the account id from the access key; "test" maps to the
	// default account. Region defaults to us-east-1. Both are only defaults —
	// a user-set value (or eksctl's own --region flag) takes precedence.
	setIfAbsent(&env, "AWS_ACCESS_KEY_ID", "test")
	setIfAbsent(&env, "AWS_SECRET_ACCESS_KEY", "test")
	setIfAbsent(&env, "AWS_DEFAULT_REGION", "us-east-1")
	setIfAbsent(&env, "AWS_REGION", "us-east-1")

	return env
}

func setIfAbsent(env *[]string, key, value string) {
	prefix := key + "="
	for _, e := range *env {
		if strings.HasPrefix(e, prefix) {
			return
		}
	}
	*env = append(*env, prefix+value)
}
