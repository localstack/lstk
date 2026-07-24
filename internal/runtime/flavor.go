package runtime

import (
	"path/filepath"
	"strings"
)

// runtimeFlavor identifies which container runtime is most likely backing (or
// meant to back) a given daemon socket, so EmitUnhealthyError can tailor its
// start hint instead of always assuming Docker.
type runtimeFlavor int

const (
	flavorUnknown runtimeFlavor = iota
	flavorDockerDesktop
	flavorRancherDesktop
	flavorColima
	flavorOrbstack
	flavorLima
	// flavorPodman covers Podman where rootful vs. rootless doesn't matter for the
	// suggested start command: the macOS "machine" (VM) backend (only one systemd-less
	// start command applies there) and the ambiguous case where the only evidence is
	// the podman CLI on PATH. Linux native sockets are more specific — see
	// flavorPodmanRootful/flavorPodmanRootless — since rootful and rootless Podman are
	// started with different systemctl invocations (docs/container-runtimes.md).
	flavorPodman
	flavorPodmanRootful
	flavorPodmanRootless
	flavorDockerNative
)

// Exported flavor identifiers returned by Runtime.Flavor, so packages outside
// runtime (e.g. internal/container) can tailor user-facing messages to the
// runtime in use without depending on the unexported classifier.
const (
	FlavorUnknown        = ""
	FlavorDockerDesktop  = "docker-desktop"
	FlavorRancherDesktop = "rancher-desktop"
	FlavorColima         = "colima"
	FlavorOrbstack       = "orbstack"
	FlavorLima           = "lima"
	FlavorPodman         = "podman"
	FlavorPodmanRootful  = "podman-rootful"
	FlavorPodmanRootless = "podman-rootless"
	FlavorDockerNative   = "docker"
)

func (f runtimeFlavor) String() string {
	switch f {
	case flavorDockerDesktop:
		return FlavorDockerDesktop
	case flavorRancherDesktop:
		return FlavorRancherDesktop
	case flavorColima:
		return FlavorColima
	case flavorOrbstack:
		return FlavorOrbstack
	case flavorLima:
		return FlavorLima
	case flavorPodman:
		return FlavorPodman
	case flavorPodmanRootful:
		return FlavorPodmanRootful
	case flavorPodmanRootless:
		return FlavorPodmanRootless
	case flavorDockerNative:
		return FlavorDockerNative
	default:
		return FlavorUnknown
	}
}

// vmSocketSpec pairs one vmSocketPaths() entry (a user-scoped socket path,
// relative to $HOME) with the runtime flavor it belongs to. vmSocketPaths
// (docker.go) and classifySocketFlavor both derive from this single table, so
// the path literals themselves are defined exactly once.
type vmSocketSpec struct {
	flavor  runtimeFlavor
	relPath string
}

var vmSocketSpecs = []vmSocketSpec{
	{flavorDockerDesktop, filepath.Join(".docker", "run", "docker.sock")},
	{flavorRancherDesktop, filepath.Join(".rd", "docker.sock")},
	{flavorColima, filepath.Join(".config", "colima", "default", "docker.sock")},
	{flavorColima, filepath.Join(".colima", "default", "docker.sock")},
	{flavorColima, filepath.Join(".colima", "docker.sock")},
	{flavorOrbstack, filepath.Join(".orbstack", "run", "docker.sock")},
	{flavorPodman, filepath.Join(".local", "share", "containers", "podman", "machine", "podman.sock")},
	{flavorLima, filepath.Join(".lima", "docker", "sock", "docker.sock")},
}

// classifySocketFlavor identifies which container runtime a resolved daemon host
// (e.g. "unix:///Users/x/.rd/docker.sock", or a bare socket path) is most likely
// backed by. It matches against the same tables used for socket discovery
// (vmSocketSpecs, nativeSocketPaths) plus the SDK's own default socket, so it
// stays in sync with findDockerSocket without duplicating any path literals.
// Returns flavorUnknown for hosts it doesn't recognize (including non-unix
// transports like tcp:// or npipe://).
func classifySocketFlavor(home, host string) runtimeFlavor {
	var socketPath string
	switch {
	case strings.HasPrefix(host, "unix://"):
		socketPath = strings.TrimPrefix(host, "unix://")
	case strings.HasPrefix(host, "/"):
		socketPath = host
	default:
		return flavorUnknown
	}

	for _, spec := range vmSocketSpecs {
		if socketPath == filepath.Join(home, spec.relPath) {
			return spec.flavor
		}
	}
	// nativeSocketSpecs only ever lists Podman sockets today; revisit this if
	// another native (non-VM) runtime is added to that table.
	for _, native := range nativeSocketSpecs() {
		if socketPath == native.path {
			return native.flavor
		}
	}
	if socketPath == "/var/run/docker.sock" {
		return flavorDockerNative
	}
	return flavorUnknown
}
