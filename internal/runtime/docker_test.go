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
	t.Run("standard socket returns daemon path", func(t *testing.T) {
		cli, err := client.NewClientWithOpts(client.WithHost("unix:///var/run/docker.sock"))
		require.NoError(t, err)
		rt := &DockerRuntime{client: cli}
		assert.Equal(t, "/var/run/docker.sock", rt.SocketPath())
	})

	t.Run("non-standard socket returns daemon path", func(t *testing.T) {
		cli, err := client.NewClientWithOpts(client.WithHost("unix:///home/user/.colima/default/docker.sock"))
		require.NoError(t, err)
		rt := &DockerRuntime{client: cli}
		assert.Equal(t, "/home/user/.colima/default/docker.sock", rt.SocketPath())
	})

	t.Run("orbstack socket returns daemon path", func(t *testing.T) {
		cli, err := client.NewClientWithOpts(client.WithHost("unix:///Users/user/.orbstack/run/docker.sock"))
		require.NoError(t, err)
		rt := &DockerRuntime{client: cli}
		assert.Equal(t, "/Users/user/.orbstack/run/docker.sock", rt.SocketPath())
	})
}

func TestSocketPath_ReturnsEmptyForTCPHost(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.WithHost("tcp://192.168.1.100:2375"))
	require.NoError(t, err)
	rt := &DockerRuntime{client: cli}

	assert.Equal(t, "", rt.SocketPath())
}

func TestSocketPathFromHost_ReturnsDockerSockForWindowsNamedPipe(t *testing.T) {
	assert.Equal(t, "/var/run/docker.sock", socketPathFromHost("npipe:////./pipe/docker_engine"))
}

func TestSocketPathFromHost_ExtractsUnixPath(t *testing.T) {
	assert.Equal(t, "/var/run/docker.sock", socketPathFromHost("unix:///var/run/docker.sock"))
}

func TestSocketPath_VMDetection(t *testing.T) {
	// Use /tmp directly to keep socket paths short — macOS limits unix socket paths to 104 chars
	// and t.TempDir() produces paths under /var/folders/... that exceed it.
	home, err := os.MkdirTemp("/tmp", "lstk-home-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)

	vmTests := []struct {
		name    string
		relPath string
	}{
		{"docker desktop", ".docker/run/docker.sock"},
		{"colima", ".colima/default/docker.sock"},
		{"orbstack", ".orbstack/run/docker.sock"},
		{"lima host", ".lima/docker/sock/docker.sock"},
	}

	for _, tc := range vmTests {
		t.Run(tc.name+" socket returns remapped path", func(t *testing.T) {
			sock := filepath.Join(home, filepath.FromSlash(tc.relPath))
			require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o755))
			require.NoError(t, os.WriteFile(sock, nil, 0o600))
			t.Cleanup(func() { require.NoError(t, os.Remove(sock)) })

			cli, err := client.NewClientWithOpts(client.WithHost("unix://" + sock))
			require.NoError(t, err)
			rt := &DockerRuntime{client: cli}
			assert.Equal(t, "/var/run/docker.sock", rt.SocketPath())
		})
	}

	t.Run("rootless socket returns actual path", func(t *testing.T) {
		// Use a non-VM socket path (short path to avoid Docker client limit)
		rootlessSock := "/tmp/lstk-docker.sock"
		require.NoError(t, os.WriteFile(rootlessSock, nil, 0o600))
		t.Cleanup(func() { require.NoError(t, os.Remove(rootlessSock)) })

		cli, err := client.NewClientWithOpts(client.WithHost("unix://" + rootlessSock))
		require.NoError(t, err)
		rt := &DockerRuntime{client: cli}
		assert.Equal(t, rootlessSock, rt.SocketPath())
	})
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

func TestFindDockerSocket_ProbesVMSockets(t *testing.T) {
	t.Setenv("LIMA_INSTANCE", "")

	tests := []struct {
		name    string
		relPath string
	}{
		{"docker desktop", ".docker/run/docker.sock"},
		{"colima", ".colima/default/docker.sock"},
		{"orbstack", ".orbstack/run/docker.sock"},
		{"lima host", ".lima/docker/sock/docker.sock"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			sock := filepath.Join(tmpDir, filepath.FromSlash(tc.relPath))
			require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o700))
			require.NoError(t, os.WriteFile(sock, nil, 0o600))
			t.Setenv("HOME", tmpDir)

			assert.Equal(t, sock, findDockerSocket())
		})
	}
}
