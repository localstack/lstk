package container

import (
	"net"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/snowflake"
)

// azureSubdomain mirrors azureconfig.AzureSubdomain; importing azureconfig here
// would pull the az CLI wrapper into the start path for one string.
const azureSubdomain = "azure"

// EmulatorURL returns the URL at which an emulator of type t is served, given
// the host:port from endpoint.ResolveHost. Snowflake and Azure are routed by
// subdomain, and Azure is served over https. An IP host gets no subdomain,
// since a subdomain of an IP is not a valid host; the scheme is kept.
func EmulatorURL(t config.EmulatorType, resolvedHost string) string {
	switch t {
	case config.EmulatorSnowflake:
		if h := snowflake.Hostname(resolvedHost); h != "" {
			return "http://" + h
		}
		return "http://" + resolvedHost
	case config.EmulatorAzure:
		if isIPHost(resolvedHost) {
			return "https://" + resolvedHost
		}
		return "https://" + azureSubdomain + "." + resolvedHost
	default:
		return "http://" + resolvedHost
	}
}

func isIPHost(hostPort string) bool {
	host := hostPort
	if h, _, err := net.SplitHostPort(hostPort); err == nil {
		host = h
	}
	return net.ParseIP(host) != nil
}
