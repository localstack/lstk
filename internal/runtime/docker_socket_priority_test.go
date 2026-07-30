package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveSocketAt creates a listening unix socket at home/relPath and returns its full path.
func liveSocketAt(t *testing.T, home, relPath string) string {
	t.Helper()
	sock := filepath.Join(home, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(sock), 0o700))
	listenUnixSocket(t, sock)
	return sock
}

// A live Docker socket must win over a co-installed runtime's socket.
//
// probeSocket only proves something is listening, not that it is the daemon running lstk's
// container. Where two runtimes are installed - Podman ships on GitHub's Ubuntu runner images,
// and plenty of workstations have Rancher Desktop alongside Docker - the alternative socket was
// picked first, sending lstk to a daemon with no emulator container. The user then sees
// "LocalStack ... Emulator is not running" while the container is demonstrably up.
func TestFindDockerSocket_LinuxPrefersDockerOverCoinstalledRuntime(t *testing.T) {
	home := shortTempDir(t)
	native := liveSocketAt(t, shortTempDir(t), "docker.sock")
	liveSocketAt(t, home, ".docker/run/docker.sock") // a competing runtime, also live

	assert.Equal(t, native, findDockerSocketFor("", home, native, "linux"),
		"a live Docker socket must be preferred over a co-installed runtime's socket")
}

// Without a Docker daemon the alternative-runtime probes must still work - auto-detecting
// Rancher Desktop, Colima, OrbStack and Podman is the point of that list.
func TestFindDockerSocket_LinuxFallsBackWhenDockerIsAbsent(t *testing.T) {
	home := shortTempDir(t)
	rancher := liveSocketAt(t, home, ".rd/docker.sock")
	absent := filepath.Join(t.TempDir(), "not-listening.sock")

	assert.Equal(t, rancher, findDockerSocketFor("", home, absent, "linux"),
		"without a Docker daemon the alternative-runtime probe must still be used")
}

// A socket file that exists but has no listener must not shadow a live alternative.
func TestFindDockerSocket_LinuxIgnoresStaleDockerSocket(t *testing.T) {
	home := shortTempDir(t)
	rancher := liveSocketAt(t, home, ".rd/docker.sock")

	stale := filepath.Join(t.TempDir(), "stale.sock")
	require.NoError(t, os.WriteFile(stale, nil, 0o600)) // exists, nothing listening

	assert.Equal(t, rancher, findDockerSocketFor("", home, stale, "linux"),
		"a stale socket file must fall through to the live alternative")
}

// On macOS/Windows the native path is a symlink owned by whichever VM runtime is active, so
// the VM probes stay first to keep isVM/Flavor classification (and the bind-mount rewrite) right.
func TestFindDockerSocket_NonLinuxKeepsVMProbesFirst(t *testing.T) {
	home := shortTempDir(t)
	native := liveSocketAt(t, shortTempDir(t), "docker.sock")
	desktop := liveSocketAt(t, home, ".docker/run/docker.sock")

	assert.Equal(t, desktop, findDockerSocketFor("", home, native, "darwin"),
		"on macOS the VM socket must still win so the daemon is classified as VM-backed")
}

// Lima reports the socket at the native path inside its VM, regardless of the probe order.
func TestFindDockerSocket_LimaShortCircuits(t *testing.T) {
	home := shortTempDir(t)
	liveSocketAt(t, home, ".rd/docker.sock")
	native := filepath.Join(t.TempDir(), "never-probed.sock")

	assert.Equal(t, native, findDockerSocketFor("default", home, native, "linux"))
}
