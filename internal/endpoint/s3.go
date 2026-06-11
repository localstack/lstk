package endpoint

import (
	"net/url"
	"strings"
)

// S3Addressing decides S3 path-style and the S3 endpoint for the given base
// endpoint. Virtual-host-style addressing (used by *.localstack.cloud hosts)
// places the bucket as a subdomain of the S3 endpoint host, so the S3 endpoint
// host carries an `s3.` prefix and path style is off. Non-domain hosts
// (127.0.0.1/localhost) require path style and use the bare endpoint.
//
// It is used both by the Terraform proxy (to set `s3_use_path_style` and the
// `s3` endpoint key in the generated override) and by the CDK proxy (to set
// `AWS_ENDPOINT_URL_S3`). CDK has no env-only lever for path style, so a
// pathStyle==true result there is informational only — see the cdk-proxy design.
func S3Addressing(endpointURL string) (pathStyle bool, s3Endpoint string) {
	u, err := url.Parse(endpointURL)
	if err != nil || u.Hostname() == "" {
		// Can't parse — safest default is path style with the bare endpoint.
		return true, endpointURL
	}
	if !virtualHostCapable(u.Hostname()) {
		return true, endpointURL
	}
	if strings.HasPrefix(u.Hostname(), "s3.") {
		return false, endpointURL
	}
	host := "s3." + u.Hostname()
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return false, u.Scheme + "://" + host
}

// TODO: in future, replace this with a more intelligent algorithm that tries to resolve whatever DNS
// endpoint is given.
func virtualHostCapable(host string) bool {
	return host == "localstack.cloud" || strings.HasSuffix(host, ".localstack.cloud")
}
