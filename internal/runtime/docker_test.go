package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeSocket_ReturnsFirstExisting(t *testing.T) {
	dir := t.TempDir()
	sock1 := filepath.Join(dir, "first.sock")
	sock2 := filepath.Join(dir, "second.sock")

	require.NoError(t, os.WriteFile(sock1, nil, 0o600))
	require.NoError(t, os.WriteFile(sock2, nil, 0o600))

	assert.Equal(t, sock1, probeSocket(sock1, sock2))
}

func TestProbeSocket_SkipsMissingAndReturnsExisting(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sock")
	existing := filepath.Join(dir, "existing.sock")

	require.NoError(t, os.WriteFile(existing, nil, 0o600))

	assert.Equal(t, existing, probeSocket(missing, existing))
}

func TestProbeSocket_ReturnsEmptyWhenNoneExist(t *testing.T) {
	assert.Equal(t, "", probeSocket("/no/such/path.sock", "/also/missing.sock"))
}

func TestProbeSocket_ReturnsEmptyForNoCandidates(t *testing.T) {
	assert.Equal(t, "", probeSocket())
}

func TestSocketPath_ExtractsUnixPath(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithHost("unix:///home/user/.colima/default/docker.sock"))
	require.NoError(t, err)
	rt := &DockerRuntime{client: cli}

	assert.Equal(t, "/home/user/.colima/default/docker.sock", rt.SocketPath())
}

func TestSocketPath_ReturnsEmptyForTCPHost(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://192.168.1.100:2375"))
	require.NoError(t, err)
	rt := &DockerRuntime{client: cli}

	assert.Equal(t, "", rt.SocketPath())
}

func TestWindowsDockerStartCommand_DockerAvailable(t *testing.T) {
	lookPath := func(string) (string, error) { return "/usr/bin/docker", nil }
	assert.Equal(t, "docker desktop start", windowsDockerStartCommand(func(string) string { return "" }, lookPath))
}

func TestWindowsDockerStartCommand_PowerShellFallback(t *testing.T) {
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	getenv := func(key string) string {
		if key == "PSModulePath" {
			return `C:\Windows\System32\WindowsPowerShell\v1.0\Modules`
		}
		return ""
	}
	assert.Equal(t, `& 'C:\Program Files\Docker\Docker\Docker Desktop.exe'`, windowsDockerStartCommand(getenv, lookPath))
}

func TestWindowsDockerStartCommand_CmdFallback(t *testing.T) {
	lookPath := func(string) (string, error) { return "", errors.New("not found") }
	assert.Equal(t, `"C:\Program Files\Docker\Docker\Docker Desktop.exe"`, windowsDockerStartCommand(func(string) string { return "" }, lookPath))
}

func TestFindDockerSocket_LimaVM(t *testing.T) {
	t.Setenv("LIMA_INSTANCE", "default")
	sock := findDockerSocket()
	assert.Equal(t, "/var/run/docker.sock", sock)
}

func TestFindDockerSocket_IncludesLimaPathOnHost(t *testing.T) {
	t.Setenv("LIMA_INSTANCE", "")

	tmpDir := t.TempDir()
	limaSock := filepath.Join(tmpDir, ".lima", "docker", "sock", "docker.sock")
	require.NoError(t, os.MkdirAll(filepath.Dir(limaSock), 0o700))
	require.NoError(t, os.WriteFile(limaSock, nil, 0o600))

	t.Setenv("HOME", tmpDir)

	result := findDockerSocket()
	assert.Equal(t, limaSock, result)
}
