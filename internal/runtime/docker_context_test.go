package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeContextMeta writes a minimal Docker CLI context meta.json for contextName
// under configDir, following the same contexts/meta/<sha256-hex>/meta.json layout
// the real Docker CLI uses, and returns the unix socket path it points at (a live
// listener when live is true, a stale path otherwise).
func writeContextMeta(t *testing.T, configDir, contextName string, live bool) string {
	t.Helper()

	hash := sha256.Sum256([]byte(contextName))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]))
	require.NoError(t, os.MkdirAll(metaDir, 0o700))

	sockDir := shortTempDir(t)
	sock := filepath.Join(sockDir, "docker.sock")
	if live {
		listenUnixSocket(t, sock)
	}

	meta := `{"Name":"` + contextName + `","Endpoints":{"docker":{"Host":"unix://` + sock + `"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(meta), 0o600))
	return sock
}

func writeDockerConfig(t *testing.T, configDir, currentContext string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	config := `{"currentContext":"` + currentContext + `"}`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0o600))
}

func envLookup(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestCurrentDockerContextName_DockerContextEnvWins(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "from-config")

	getenv := envLookup(map[string]string{"DOCKER_CONTEXT": "from-env"})
	assert.Equal(t, "from-env", currentDockerContextName(getenv, configDir))
}

func TestCurrentDockerContextName_FallsBackToConfigJSON(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "rancher-desktop")

	assert.Equal(t, "rancher-desktop", currentDockerContextName(envLookup(nil), configDir))
}

func TestCurrentDockerContextName_DefaultMeansNoOverride(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "default")

	assert.Equal(t, "", currentDockerContextName(envLookup(nil), configDir))
	assert.Equal(t, "", currentDockerContextName(envLookup(map[string]string{"DOCKER_CONTEXT": "default"}), configDir))
}

func TestCurrentDockerContextName_NoConfigFile(t *testing.T) {
	configDir := shortTempDir(t)
	assert.Equal(t, "", currentDockerContextName(envLookup(nil), configDir))
}

func TestCurrentDockerContextName_MalformedConfigFile(t *testing.T) {
	configDir := shortTempDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.json"), []byte("not json"), 0o600))
	assert.Equal(t, "", currentDockerContextName(envLookup(nil), configDir))
}

func TestDockerContextEndpoint_ParsesMetaJSON(t *testing.T) {
	configDir := shortTempDir(t)
	sock := writeContextMeta(t, configDir, "rancher-desktop", false)

	assert.Equal(t, "unix://"+sock, dockerContextEndpoint(configDir, "rancher-desktop"))
}

func TestDockerContextEndpoint_MissingMetaFile(t *testing.T) {
	configDir := shortTempDir(t)
	assert.Equal(t, "", dockerContextEndpoint(configDir, "nonexistent"))
}

func TestDockerContextEndpoint_MalformedMetaFile(t *testing.T) {
	configDir := shortTempDir(t)
	hash := sha256.Sum256([]byte("broken"))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]))
	require.NoError(t, os.MkdirAll(metaDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte("not json"), 0o600))

	assert.Equal(t, "", dockerContextEndpoint(configDir, "broken"))
}

func TestResolveDockerContextHost_LiveUnixSocket(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "rancher-desktop")
	sock := writeContextMeta(t, configDir, "rancher-desktop", true)

	assert.Equal(t, "unix://"+sock, resolveDockerContextHost(envLookup(nil), configDir))
}

func TestResolveDockerContextHost_StaleSocketFallsThrough(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "rancher-desktop")
	writeContextMeta(t, configDir, "rancher-desktop", false)

	assert.Equal(t, "", resolveDockerContextHost(envLookup(nil), configDir),
		"a context pointing at a socket with no listener must fall through to the probe list")
}

func TestResolveDockerContextHost_NoContextOverride(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "default")

	assert.Equal(t, "", resolveDockerContextHost(envLookup(nil), configDir))
}

func TestResolveDockerContextHost_NoConfigAtAll(t *testing.T) {
	configDir := shortTempDir(t)
	assert.Equal(t, "", resolveDockerContextHost(envLookup(nil), configDir))
}

func TestResolveDockerContextHost_NonUnixEndpointIgnored(t *testing.T) {
	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "remote")

	hash := sha256.Sum256([]byte("remote"))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]))
	require.NoError(t, os.MkdirAll(metaDir, 0o700))
	meta := `{"Name":"remote","Endpoints":{"docker":{"Host":"tcp://192.168.1.50:2375"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(meta), 0o600))

	assert.Equal(t, "", resolveDockerContextHost(envLookup(nil), configDir),
		"only unix (and npipe on windows) endpoints are honored; anything else falls through")
}

func TestDefaultDockerConfigDir_UsesDockerConfigEnvVar(t *testing.T) {
	assert.Equal(t, "/custom/docker/config", defaultDockerConfigDir(envLookup(map[string]string{"DOCKER_CONFIG": "/custom/docker/config"})))
}

func TestDefaultDockerConfigDir_FallsBackToHomeDotDocker(t *testing.T) {
	home := shortTempDir(t)
	t.Setenv("HOME", home)

	assert.Equal(t, filepath.Join(home, ".docker"), defaultDockerConfigDir(envLookup(nil)))
}

// Confirms a context that happens to resolve to one of the hardcoded VM socket
// paths (e.g. Rancher Desktop's ~/.rd/docker.sock) still makes isVM() true, so
// SocketPath() returns the /var/run/docker.sock rewrite for the container bind-mount.
func TestIsVM_ContextResolvedRancherSocketIsTreatedAsVM(t *testing.T) {
	home := shortTempDir(t)
	sock := filepath.Join(home, ".rd", "docker.sock")
	require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o700))
	listenUnixSocket(t, sock)
	t.Setenv("HOME", home)

	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "rancher-desktop")

	hash := sha256.Sum256([]byte("rancher-desktop"))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]))
	require.NoError(t, os.MkdirAll(metaDir, 0o700))
	meta := `{"Name":"rancher-desktop","Endpoints":{"docker":{"Host":"unix://` + sock + `"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(meta), 0o600))

	host := resolveDockerContextHost(envLookup(nil), configDir)
	require.Equal(t, "unix://"+sock, host)

	cli, err := client.New(client.WithHost(host))
	require.NoError(t, err)
	rt := &DockerRuntime{client: cli}

	assert.True(t, rt.isVM())
	assert.Equal(t, "/var/run/docker.sock", rt.SocketPath())
}

// A context resolving to a native (non-VM) podman socket must NOT be treated as a
// VM: the bind-mounted path for Lambda containers must be the real host socket.
func TestIsVM_ContextResolvedNativePodmanSocketIsNotTreatedAsVM(t *testing.T) {
	sockDir := shortTempDir(t)
	sock := filepath.Join(sockDir, "podman.sock")
	listenUnixSocket(t, sock)

	configDir := shortTempDir(t)
	writeDockerConfig(t, configDir, "podman")

	hash := sha256.Sum256([]byte("podman"))
	metaDir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(hash[:]))
	require.NoError(t, os.MkdirAll(metaDir, 0o700))
	meta := `{"Name":"podman","Endpoints":{"docker":{"Host":"unix://` + sock + `"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(metaDir, "meta.json"), []byte(meta), 0o600))

	host := resolveDockerContextHost(envLookup(nil), configDir)
	require.Equal(t, "unix://"+sock, host)

	cli, err := client.New(client.WithHost(host))
	require.NoError(t, err)
	rt := &DockerRuntime{client: cli}

	assert.False(t, rt.isVM())
	assert.Equal(t, sock, rt.SocketPath())
}
