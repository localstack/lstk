package snowflake

import (
	"net"

	"github.com/localstack/lstk/internal/endpoint"
)

// S3Endpoint returns the value for the Snowflake emulator's SF_S3_ENDPOINT
// variable for the given host port. The Python Snowflake emulator funnels all
// S3 access (including internal stages, e.g. COPY INTO) through this single
// endpoint, so it must match the port the emulator is exposed on; otherwise it
// defaults to 4566 and S3 access fails when the emulator runs on a custom port.
func S3Endpoint(port string) string {
	return "s3." + endpoint.Hostname + ":" + port
}

func Hostname(resolvedHost string) string {
	host, _, err := net.SplitHostPort(resolvedHost)
	if err != nil {
		return ""
	}
	if net.ParseIP(host) != nil {
		// Returns "" when resolvedHost is an IP, since prepending a subdomain to an IP is invalid.
		return ""
	}
	return "snowflake." + resolvedHost
}
