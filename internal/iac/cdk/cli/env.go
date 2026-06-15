package cli

import "os"

// Environment variables this package reads. They are process environment, not
// lstk config, so reading them here (the domain boundary for cdk) is consistent
// with the rule that domain code must not call config.Get().

// cdkCmd returns the CDK binary name to invoke, honoring LSTK_CDK_CMD and
// defaulting to "cdk".
func cdkCmd() string {
	if v := os.Getenv("LSTK_CDK_CMD"); v != "" {
		return v
	}
	return "cdk"
}

// endpointURLOverride returns AWS_ENDPOINT_URL, which takes precedence over the
// auto-resolved LocalStack endpoint.
func endpointURLOverride() string {
	return os.Getenv("AWS_ENDPOINT_URL")
}

// s3EndpointOverride returns AWS_ENDPOINT_URL_S3, which takes precedence over
// the endpoint derived from the base endpoint's host.
func s3EndpointOverride() string {
	return os.Getenv("AWS_ENDPOINT_URL_S3")
}
