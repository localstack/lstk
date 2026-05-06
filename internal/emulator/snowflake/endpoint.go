package snowflake

import "net"

func Endpoint(resolvedHost string) string {
	host, _, err := net.SplitHostPort(resolvedHost)
	if err != nil {
		return ""
	}
	if net.ParseIP(host) != nil {
		// Returns "" when resolvedHost is an IP, since prepending a subdomain to an IP is invalid.
		return ""
	}
	return "http://snowflake." + resolvedHost
}
