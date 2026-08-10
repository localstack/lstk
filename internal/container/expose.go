package container

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// mergeExposePorts appends the publications requested via the config's expose_ports
// to mappings. Ports lstk already publishes on its own (the edge port, the extra
// gateway ports, the 4510-4559 service range) cannot be re-declared: Docker keys
// bindings by container port and by host port, so a second declaration of either
// side would silently discard one of the two. Such an entry is skipped, and warned
// about whenever skipping it means the user's mapping does not happen — a redundant
// entry that asks for exactly what lstk already does is dropped quietly.
//
// Mappings are never Optional: expose_ports is an explicit request, so a busy or
// unbindable host port is a hard failure rather than a silent degradation (unlike
// the 443 lstk adds on its own).
func mergeExposePorts(sink output.Sink, mappings []runtime.PortMapping, primaryContainerPort, primaryHostPort string, exposed []config.PortSpec) []runtime.PortMapping {
	hostForContainer := map[string]string{primaryContainerPort + "/tcp": primaryHostPort}
	containerForHost := map[string]string{primaryHostPort + "/tcp": primaryContainerPort}
	for _, m := range mappings {
		proto := m.Protocol
		if proto == "" {
			proto = "tcp"
		}
		hostForContainer[m.ContainerPort+"/"+proto] = m.HostPort
		containerForHost[m.HostPort+"/"+proto] = m.ContainerPort
	}

	warn := func(text string) {
		sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: text})
	}

	for _, e := range exposed {
		containerKey := e.ContainerPort + "/" + e.Protocol
		hostKey := e.HostPort + "/" + e.Protocol
		if host, ok := hostForContainer[containerKey]; ok {
			if host != e.HostPort {
				warn(fmt.Sprintf(
					"Ignoring expose_ports entry %s — lstk already publishes container port %s on host port %s.",
					e, containerKey, host))
			}
			continue
		}
		if container, ok := containerForHost[hostKey]; ok {
			warn(fmt.Sprintf(
				"Ignoring expose_ports entry %s — host port %s is already used to publish container port %s.",
				e, hostKey, container))
			continue
		}
		hostForContainer[containerKey] = e.HostPort
		containerForHost[hostKey] = e.ContainerPort
		mappings = append(mappings, runtime.PortMapping{
			ContainerPort: e.ContainerPort,
			HostPort:      e.HostPort,
			Protocol:      e.Protocol,
		})
	}
	return mappings
}
