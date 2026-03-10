package endpoint

import "net"

const Hostname = "localhost.localstack.cloud"

// ResolveHost returns the best host:port for reaching LocalStack on the given port.
// If override is non-empty it is returned as-is. Otherwise a DNS check is performed;
// if Hostname does not resolve to 127.0.0.1 (e.g. DNS rebind protection is active),
// it falls back to 127.0.0.1 directly.
func ResolveHost(port, override string) (host string, dnsOK bool) {
	if override != "" {
		return override, true
	}
	// Use a "test." subdomain: *.localhost.localstack.cloud has wildcard DNS that resolves
	// to 127.0.0.1, so any subdomain works as a probe without hitting the actual service.
	addrs, err := net.LookupHost("test." + Hostname)
	if err != nil {
		return "127.0.0.1:" + port, false
	}
	for _, addr := range addrs {
		if addr == "127.0.0.1" {
			return Hostname + ":" + port, true
		}
	}
	return "127.0.0.1:" + port, false
}
