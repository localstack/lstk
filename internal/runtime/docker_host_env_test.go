package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An explicit DOCKER_HOST must win over runtime auto-detection.
//
// NewDockerRuntime starts from client.FromEnv (which honours DOCKER_HOST) but then, whenever the
// dockerHost argument is empty, appends client.WithHost(...) for whatever the context lookup or
// socket probe found. Later options win, so a probed socket silently replaces the daemon the user
// named - and lstk talks to the wrong daemon, reporting a running emulator as absent.
//
// This is reachable wherever another runtime's socket happens to be live alongside Docker: CI
// images that ship Podman, or a workstation with Rancher Desktop installed but Docker in use.
func TestNewDockerRuntime_DockerHostEnvBeatsSocketProbe(t *testing.T) {
	// A live socket at a probed VM path - what the auto-detection would latch onto.
	home := t.TempDir()
	probed := filepath.Join(home, ".docker", "run", "docker.sock")
	require.NoError(t, os.MkdirAll(filepath.Dir(probed), 0o700))
	listenUnixSocket(t, probed)
	t.Setenv("HOME", home)
	t.Setenv("LIMA_INSTANCE", "")
	// No Docker CLI context, so resolution falls through to the socket probe.
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")

	// The daemon the operator explicitly asked for.
	chosen := filepath.Join(t.TempDir(), "chosen.sock")
	listenUnixSocket(t, chosen)
	t.Setenv("DOCKER_HOST", "unix://"+chosen)

	rt, err := NewDockerRuntime("")
	require.NoError(t, err)

	assert.Equal(t, "unix://"+chosen, rt.client.DaemonHost(),
		"DOCKER_HOST was set explicitly but auto-detection overrode it with a probed socket")
}

// With no operator preference, auto-detection decides - and whatever it picks is what the
// client must actually be pointed at. The expected value is taken from findDockerSocket rather
// than hardcoded, since which socket wins legitimately depends on the host (a machine with a
// live Docker daemon prefers it over the temp socket created here; see the ordering tests in
// docker_socket_priority_test.go, which cover that choice deterministically).
func TestNewDockerRuntime_SocketProbeUsedWhenNoDockerHost(t *testing.T) {
	home := t.TempDir()
	probed := filepath.Join(home, ".docker", "run", "docker.sock")
	require.NoError(t, os.MkdirAll(filepath.Dir(probed), 0o700))
	listenUnixSocket(t, probed)
	t.Setenv("HOME", home)
	t.Setenv("LIMA_INSTANCE", "")
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_HOST", "")

	detected := findDockerSocket()
	require.NotEmpty(t, detected, "no socket detected, so there is nothing to assert about")

	rt, err := NewDockerRuntime("")
	require.NoError(t, err)

	assert.Equal(t, "unix://"+detected, rt.client.DaemonHost(),
		"with no DOCKER_HOST the auto-detected socket should be used")
}
