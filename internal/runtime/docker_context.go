package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	stdruntime "runtime"
	"strings"
)

// defaultDockerConfigDir resolves the directory the Docker CLI reads its config and
// context metadata from: $DOCKER_CONFIG if set, else ~/.docker.
func defaultDockerConfigDir(getenv func(string) string) string {
	if dir := getenv("DOCKER_CONFIG"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docker")
}

// resolveDockerContextHost returns the Docker daemon endpoint (e.g.
// "unix:///path/to/docker.sock") configured by the current Docker CLI context, or ""
// if there is no context override, its metadata can't be read, or (for a unix
// socket) the daemon isn't actually reachable there.
//
// Resolving via the CLI context is more robust than a hardcoded socket list since
// Rancher Desktop and OrbStack both register themselves as contexts and keep that
// registration current across upgrades/reinstalls. Podman is covered independently
// via the native/VM socket probe list (see nativeSocketSpecs/vmSocketSpecs) rather
// than via context registration — Podman Desktop is not known to register a Docker
// CLI context today.
//
// getenv and configDir are injected for testability, following the same style as
// windowsDockerStartCommand.
func resolveDockerContextHost(getenv func(string) string, configDir string) string {
	name := currentDockerContextName(getenv, configDir)
	if name == "" {
		return ""
	}

	endpoint := dockerContextEndpoint(configDir, name)
	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		sock := strings.TrimPrefix(endpoint, "unix://")
		// A context can outlive the daemon it points at (e.g. the VM behind it was
		// deleted) — probe it like any other socket candidate so a stale context
		// falls through to the regular probe list instead of hard-failing.
		if probeSocket(sock) == "" {
			return ""
		}
		return endpoint
	case strings.HasPrefix(endpoint, "npipe://") && stdruntime.GOOS == "windows":
		// Same rationale as the unix branch above: a context can outlive the
		// daemon it points at, so probe reachability before honoring it.
		if !probeNamedPipe(endpoint) {
			return ""
		}
		return endpoint
	default:
		return ""
	}
}

// currentDockerContextName returns the active Docker CLI context name, or "" if
// there is none (unset, "default", or unreadable/malformed config).
func currentDockerContextName(getenv func(string) string, configDir string) string {
	if name := getenv("DOCKER_CONTEXT"); name != "" {
		return normalizeContextName(name)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return normalizeContextName(cfg.CurrentContext)
}

// normalizeContextName treats "default" the same as unset: it's the daemon the SDK
// already targets without any override.
func normalizeContextName(name string) string {
	if name == "default" {
		return ""
	}
	return name
}

// dockerContextEndpoint reads the Docker endpoint host configured for the named
// context, per the on-disk layout the Docker CLI itself uses: metadata for a context
// lives under contexts/meta/<sha256-hex-of-name>/meta.json.
func dockerContextEndpoint(configDir, name string) string {
	hash := sha256.Sum256([]byte(name))
	metaPath := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]), "meta.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	var meta struct {
		Endpoints struct {
			Docker struct {
				Host string `json:"Host"`
			} `json:"docker"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.Endpoints.Docker.Host
}
