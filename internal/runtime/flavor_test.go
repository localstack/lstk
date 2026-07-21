package runtime

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifySocketFlavor_VMSockets(t *testing.T) {
	home := "/home/user"

	tests := []struct {
		name     string
		relPath  string
		expected runtimeFlavor
	}{
		{"docker desktop", filepath.Join(".docker", "run", "docker.sock"), flavorDockerDesktop},
		{"rancher desktop", filepath.Join(".rd", "docker.sock"), flavorRancherDesktop},
		{"colima xdg config", filepath.Join(".config", "colima", "default", "docker.sock"), flavorColima},
		{"colima legacy default", filepath.Join(".colima", "default", "docker.sock"), flavorColima},
		{"colima legacy bare", filepath.Join(".colima", "docker.sock"), flavorColima},
		{"orbstack", filepath.Join(".orbstack", "run", "docker.sock"), flavorOrbstack},
		{"podman machine", filepath.Join(".local", "share", "containers", "podman", "machine", "podman.sock"), flavorPodman},
		{"lima", filepath.Join(".lima", "docker", "sock", "docker.sock"), flavorLima},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host := "unix://" + filepath.Join(home, tc.relPath)
			assert.Equal(t, tc.expected, classifySocketFlavor(home, host))
		})
	}
}

func TestClassifySocketFlavor_NativePodmanSockets(t *testing.T) {
	t.Run("rootful", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		assert.Equal(t, flavorPodmanRootful, classifySocketFlavor("/home/user", "unix:///run/podman/podman.sock"))
	})

	t.Run("rootless", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
		assert.Equal(t, flavorPodmanRootless, classifySocketFlavor("/home/user", "unix:///run/user/1000/podman/podman.sock"))
	})
}

func TestClassifySocketFlavor_DefaultDockerSocketIsNative(t *testing.T) {
	assert.Equal(t, flavorDockerNative, classifySocketFlavor("/home/user", "unix:///var/run/docker.sock"))
}

func TestClassifySocketFlavor_UnrecognizedUnixSocketIsUnknown(t *testing.T) {
	assert.Equal(t, flavorUnknown, classifySocketFlavor("/home/user", "unix:///some/other/custom.sock"))
}

func TestClassifySocketFlavor_NonUnixTransportsAreUnknown(t *testing.T) {
	assert.Equal(t, flavorUnknown, classifySocketFlavor("/home/user", "tcp://192.168.1.100:2375"))
	assert.Equal(t, flavorUnknown, classifySocketFlavor("/home/user", "npipe:////./pipe/docker_engine"))
}
