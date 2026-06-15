package mcpconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildNPXServerSpec(t *testing.T) {
	spec := BuildNPXServerSpec("ls-token", nil)

	assert.Equal(t, "npx", spec.Command)
	assert.Equal(t, []string{"-y", "@localstack/localstack-mcp-server"}, spec.Args)
	assert.Equal(t, map[string]string{"LOCALSTACK_AUTH_TOKEN": "ls-token"}, spec.Env)
}

func TestBuildNPXServerSpecWithExtraEnv(t *testing.T) {
	spec := BuildNPXServerSpec("ls-token", map[string]string{"DEBUG": "1"})

	assert.Equal(t, map[string]string{
		"LOCALSTACK_AUTH_TOKEN": "ls-token",
		"DEBUG":                 "1",
	}, spec.Env)
}

func TestWindowsSpawnSafeSpec(t *testing.T) {
	wrapped := windowsSpawnSafeSpec(BuildNPXServerSpec("ls-token", nil))
	assert.Equal(t, "cmd", wrapped.Command)
	assert.Equal(t, []string{"/c", "npx", "-y", "@localstack/localstack-mcp-server"}, wrapped.Args)
	assert.Equal(t, map[string]string{"LOCALSTACK_AUTH_TOKEN": "ls-token"}, wrapped.Env)

	docker := BuildDockerServerSpec("ls-token", nil, DockerOptions{CacheDir: "/c", ImageTag: "latest"})
	assert.Equal(t, docker, windowsSpawnSafeSpec(docker), "non-npx specs pass through unchanged")
}

func TestBuildDockerServerSpec(t *testing.T) {
	spec := BuildDockerServerSpec("ls-token", nil, DockerOptions{
		CacheDir: "/cache",
		ImageTag: "latest",
	})

	assert.Equal(t, "docker", spec.Command)
	assert.Equal(t, []string{
		"run", "-i", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", "/cache:/cache",
		"-e", "XDG_CACHE_HOME=/cache",
		"--add-host", "host.docker.internal:host-gateway",
		"--add-host", "s3.host.docker.internal:host-gateway",
		"--add-host", "snowflake.localhost.localstack.cloud:host-gateway",
		"-e", "LOCALSTACK_AUTH_TOKEN",
		"-e", "LOCALSTACK_HOSTNAME=host.docker.internal",
		"localstack/localstack-mcp-server:latest",
	}, spec.Args)
	assert.Equal(t, map[string]string{"LOCALSTACK_AUTH_TOKEN": "ls-token"}, spec.Env)
}

func TestBuildDockerServerSpecWithWorkspaceAndExtraEnv(t *testing.T) {
	spec := BuildDockerServerSpec("ls-token", map[string]string{"DEBUG": "1", "ANOTHER": "x"}, DockerOptions{
		CacheDir:     "/cache",
		WorkspaceDir: "/work",
		ImageTag:     "1.2.3",
	})

	assert.Equal(t, []string{
		"run", "-i", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", "/cache:/cache",
		"-e", "XDG_CACHE_HOME=/cache",
		"--add-host", "host.docker.internal:host-gateway",
		"--add-host", "s3.host.docker.internal:host-gateway",
		"--add-host", "snowflake.localhost.localstack.cloud:host-gateway",
		"-e", "LOCALSTACK_AUTH_TOKEN",
		"-e", "LOCALSTACK_HOSTNAME=host.docker.internal",
		// Extra env keys are forwarded in sorted order for determinism.
		"-e", "ANOTHER",
		"-e", "DEBUG",
		"-v", "/work:/work",
		"localstack/localstack-mcp-server:1.2.3",
	}, spec.Args)
	assert.Equal(t, map[string]string{
		"LOCALSTACK_AUTH_TOKEN": "ls-token",
		"DEBUG":                 "1",
		"ANOTHER":               "x",
	}, spec.Env)
}
