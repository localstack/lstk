package container

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/localstack/lstk/internal/runtime"
)

// defaultGatewayListen is the value lstk uses when the user has not supplied a
// GATEWAY_LISTEN of their own. The gateway listens on the AWS edge port (4566)
// and on 443 for TLS routing.
const defaultGatewayListen = ":4566,:443"

// defaultBindHost is the host IP that published ports are bound to when the
// GATEWAY_LISTEN entries carry no explicit host (i.e. they listen on all
// interfaces inside the container, but are only exposed on loopback).
const defaultBindHost = "127.0.0.1"

// gatewayEntry is a single "[host]:port" pair from GATEWAY_LISTEN. An empty host
// means "all interfaces" inside the container.
type gatewayEntry struct {
	host string
	port string
}

// gatewayListen is the parsed form of a GATEWAY_LISTEN value.
type gatewayListen []gatewayEntry

// parseGatewayListen parses a GATEWAY_LISTEN value of the form
// "[host]:port[,[host]:port...]" (e.g. ":4566,:443" or "0.0.0.0:4566,0.0.0.0:443").
// An empty value falls back to defaultGatewayListen.
func parseGatewayListen(value string) (gatewayListen, error) {
	if strings.TrimSpace(value) == "" {
		value = defaultGatewayListen
	}

	var entries gatewayListen
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, port, err := splitHostPort(part)
		if err != nil {
			return nil, err
		}
		entries = append(entries, gatewayEntry{host: host, port: port})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("GATEWAY_LISTEN %q has no valid entries", value)
	}
	return entries, nil
}

// splitHostPort splits a single GATEWAY_LISTEN entry into host and port. A bare
// port ("4566") is treated as an empty host. The port must be numeric and the
// host, when present, a valid IP address.
func splitHostPort(entry string) (host, port string, err error) {
	s := entry
	if !strings.Contains(s, ":") {
		s = ":" + s
	}
	host, port, err = net.SplitHostPort(s)
	if err != nil {
		return "", "", fmt.Errorf("invalid GATEWAY_LISTEN entry %q: %w", entry, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid port in GATEWAY_LISTEN entry %q", entry)
	}
	if host != "" {
		if _, err := netip.ParseAddr(host); err != nil {
			return "", "", fmt.Errorf("invalid host in GATEWAY_LISTEN entry %q: %w", entry, err)
		}
	}
	return host, port, nil
}

// bindHost returns the host IP to bind published ports to, taken from the first
// entry (mirroring the v1 CLI). A blank host defaults to loopback so the
// historical behaviour is preserved unless the user opts into wider exposure.
func (g gatewayListen) bindHost() string {
	if len(g) == 0 || g[0].host == "" {
		return defaultBindHost
	}
	return g[0].host
}

// containerEnvValue compiles the GATEWAY_LISTEN value passed into the container.
// A loopback host is blanked so the gateway still listens on all interfaces
// inside the container even when the host only publishes it on loopback; any
// other host is kept verbatim.
func (g gatewayListen) containerEnvValue() string {
	parts := make([]string, len(g))
	for i, e := range g {
		host := e.host
		if host == defaultBindHost {
			host = ""
		}
		parts[i] = net.JoinHostPort(host, e.port)
	}
	return strings.Join(parts, ",")
}

// extraGatewayPorts returns the gateway ports that must be published in addition
// to the primary edge port (which is mapped separately via the container's
// configured host port). Each is exposed host-port == container-port. Ports
// already covered by the primary port or the service port range are skipped
// so callers don't publish the same container port twice.
func (g gatewayListen) extraGatewayPorts(primaryPort string) []runtime.PortMapping {
	var ports []runtime.PortMapping
	seen := map[string]bool{primaryPort: true}
	for _, e := range g {
		if seen[e.port] || inServicePortRange(e.port) {
			continue
		}
		seen[e.port] = true
		ports = append(ports, runtime.PortMapping{ContainerPort: e.port, HostPort: e.port})
	}
	return ports
}

// inServicePortRange reports whether port falls in the 4510-4559 range already
// published by servicePortRange.
func inServicePortRange(port string) bool {
	p, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return p >= servicePortRangeStart && p <= servicePortRangeEnd
}
