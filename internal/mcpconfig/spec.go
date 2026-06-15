// Package mcpconfig configures MCP clients (Cursor, Claude Code, VS Code, ...)
// to launch the LocalStack MCP server. It mirrors the standalone setup wizard
// shipped in @localstack/localstack-mcp-server so that an entry written here is
// interchangeable with one written by `npx @localstack/localstack-mcp-server init`.
package mcpconfig

import "sort"

const (
	// ServerName is the entry key written into every client's config. Keeping
	// it identical to the npm wizard's makes the two installers interchangeable.
	ServerName   = "localstack"
	NPMPackage   = "@localstack/localstack-mcp-server"
	DockerImage  = "localstack/localstack-mcp-server"
	AuthTokenEnv = "LOCALSTACK_AUTH_TOKEN"
)

// Method selects how the MCP server process is launched by the client.
type Method string

const (
	MethodDocker Method = "docker"
	MethodNPX    Method = "npx"
)

// ServerSpec describes how a client should launch the LocalStack MCP server.
type ServerSpec struct {
	Command string
	Args    []string
	Env     map[string]string
}

// DockerOptions tunes the `docker run` invocation written into client configs.
type DockerOptions struct {
	CacheDir     string
	WorkspaceDir string // empty means no workspace mount
	ImageTag     string
}

// windowsSpawnSafeSpec wraps an npx command in `cmd /c` so clients that spawn
// the server without a shell (Claude Code, Codex) can resolve the npx.cmd shim
// on native Windows. Non-npx and non-Windows specs pass through unchanged. This
// mirrors the npm wizard so the two installers stay interchangeable.
func windowsSpawnSafeSpec(spec ServerSpec) ServerSpec {
	if spec.Command != "npx" {
		return spec
	}
	return ServerSpec{
		Command: "cmd",
		Args:    append([]string{"/c", spec.Command}, spec.Args...),
		Env:     spec.Env,
	}
}

// BuildNPXServerSpec returns the spec for running the server via npx on the host.
func BuildNPXServerSpec(token string, extraEnv map[string]string) ServerSpec {
	return ServerSpec{
		Command: "npx",
		Args:    []string{"-y", NPMPackage},
		Env:     mergeEnv(token, extraEnv),
	}
}

// BuildDockerServerSpec returns the spec for running the server in a container.
// The server reaches the host's LocalStack via host.docker.internal and manages
// containers through the mounted Docker socket.
func BuildDockerServerSpec(token string, extraEnv map[string]string, opts DockerOptions) ServerSpec {
	args := []string{
		"run", "-i", "--rm",
		"-v", "/var/run/docker.sock:/var/run/docker.sock",
		"-v", opts.CacheDir + ":" + opts.CacheDir,
		"-e", "XDG_CACHE_HOME=" + opts.CacheDir,
		"--add-host", "host.docker.internal:host-gateway",
		"--add-host", "s3.host.docker.internal:host-gateway",
		"--add-host", "snowflake.localhost.localstack.cloud:host-gateway",
		"-e", AuthTokenEnv,
		"-e", "LOCALSTACK_HOSTNAME=host.docker.internal",
	}

	// Forward extra config vars by name; values stay in the env block so client
	// UIs don't display them. Sorted for deterministic output.
	for _, key := range sortedKeys(extraEnv) {
		args = append(args, "-e", key)
	}

	if opts.WorkspaceDir != "" {
		args = append(args, "-v", opts.WorkspaceDir+":"+opts.WorkspaceDir)
	}

	args = append(args, DockerImage+":"+opts.ImageTag)

	return ServerSpec{
		Command: "docker",
		Args:    args,
		Env:     mergeEnv(token, extraEnv),
	}
}

func mergeEnv(token string, extraEnv map[string]string) map[string]string {
	env := map[string]string{AuthTokenEnv: token}
	for k, v := range extraEnv {
		env[k] = v
	}
	return env
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
