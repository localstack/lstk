package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/localstack/lstk/test/integration/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMCPInitConfiguresClientAndEmitsTelemetry exercises the file-based client
// path end to end: a single `lstk mcp init` writes a valid, token-bearing entry
// to the client's config and emits the lstk_command telemetry event.
func TestMCPInitConfiguresClientAndEmitsTelemetry(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	analyticsSrv, events := mockAnalyticsServer(t)

	environ := env.Environ(testEnvWithHome(home, "")).
		With(env.AuthToken, "ls-test-token").
		With(env.AnalyticsEndpoint, analyticsSrv.URL)

	stdout, stderr, err := runLstk(t, testContext(t), "", environ,
		"mcp", "init", "--method", "npx", "--client", "cursor")
	require.NoError(t, err, "mcp init failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "Cursor")

	path := filepath.Join(home, ".cursor", "mcp.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "expected %s to be written", path)

	var root struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &root))
	entry, ok := root.MCPServers["localstack"]
	require.True(t, ok, "localstack entry must be present in %s", path)
	assert.Equal(t, "npx", entry.Command)
	assert.Equal(t, []string{"-y", "@localstack/localstack-mcp-server"}, entry.Args)
	assert.Equal(t, "ls-test-token", entry.Env["LOCALSTACK_AUTH_TOKEN"])

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		// Unix mode bits aren't meaningfully enforced on Windows (ACL-based).
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "token-bearing config must not be world-readable")
	}

	assertCommandTelemetry(t, events, "mcp init", 0)
}

// TestMCPInitRequiresToken: with no token available the command fails fast with
// actionable guidance instead of writing a broken (token-less) config.
func TestMCPInitRequiresToken(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	environ := env.Environ(testEnvWithHome(home, "")).Without(env.AuthToken)

	stdout, _, err := runLstk(t, testContext(t), "", environ,
		"mcp", "init", "--method", "npx", "--client", "cursor")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "No LocalStack auth token")
	assert.Contains(t, stdout, "lstk login")

	_, statErr := os.Stat(filepath.Join(home, ".cursor", "mcp.json"))
	assert.True(t, os.IsNotExist(statErr), "no config should be written when the token is missing")
}

// TestMCPInitUnknownClient: an unrecognized --client value is rejected and lists
// the supported clients.
func TestMCPInitUnknownClient(t *testing.T) {
	t.Parallel()
	home := t.TempDir()

	environ := env.Environ(testEnvWithHome(home, "")).With(env.AuthToken, "ls-test-token")

	stdout, _, err := runLstk(t, testContext(t), "", environ,
		"mcp", "init", "--client", "bogus")
	require.Error(t, err)
	requireExitCode(t, 1, err)
	assert.Contains(t, stdout, "Unknown MCP client")
	assert.Contains(t, stdout, "cursor")
}
