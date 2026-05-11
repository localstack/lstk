package endpoint

import (
	"context"
	"net"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

const Hostname = "localhost.localstack.cloud"

const DNSRebindNote = "Could not resolve localhost.localstack.cloud, falling back to 127.0.0.1."

// ResolveHost returns the best host:port for reaching LocalStack on the given port.
// If override is non-empty it is returned as-is. Otherwise a DNS check is performed;
// if Hostname does not resolve to 127.0.0.1 (e.g. DNS rebind protection is active),
// it falls back to 127.0.0.1 directly.
func ResolveHost(ctx context.Context, port, override string) (host string, dnsOK bool) {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/endpoint").Start(ctx, "endpoint.ResolveHost")
	defer span.End()

	if override != "" {
		span.SetAttributes(attribute.String("endpoint.host", override), attribute.Bool("endpoint.override", true))
		return override, true
	}
	// Use a "test." subdomain: *.localhost.localstack.cloud has wildcard DNS that resolves
	// to 127.0.0.1, so any subdomain works as a probe without hitting the actual service.
	probe := "test." + Hostname
	span.SetAttributes(attribute.String("endpoint.dns.probe", probe))
	resolver := net.Resolver{}
	addrs, err := resolver.LookupHost(ctx, probe)
	if err != nil {
		span.SetAttributes(attribute.Bool("endpoint.dns.ok", false))
		return "127.0.0.1:" + port, false
	}
	for _, addr := range addrs {
		if addr == "127.0.0.1" {
			span.SetAttributes(attribute.Bool("endpoint.dns.ok", true), attribute.String("endpoint.host", Hostname))
			return Hostname + ":" + port, true
		}
	}
	span.SetAttributes(attribute.Bool("endpoint.dns.ok", false))
	return "127.0.0.1:" + port, false
}
