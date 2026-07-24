package mcpconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func TestFileClientInstallCursor(t *testing.T) {
	home := t.TempDir()
	ctx := ctxFor("darwin", home, nil)
	spec := BuildNPXServerSpec("ls-token", nil)

	outcome := cursorAdapter().Install(context.Background(), spec, ctx)
	require.Equal(t, statusInstalled, outcome.Status, outcome.Detail)

	path := filepath.Join(home, ".cursor", "mcp.json")
	root := readJSON(t, path)
	entry := root["mcpServers"].(map[string]any)["localstack"].(map[string]any)
	assert.Equal(t, "npx", entry["command"])
	env := entry["env"].(map[string]any)
	assert.Equal(t, "ls-token", env["LOCALSTACK_AUTH_TOKEN"])

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		// Unix mode bits aren't meaningfully enforced on Windows (ACL-based).
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "token-bearing file must not be world-readable")
	}
}

func TestFileClientInstallVSCodeUsesServersKeyAndType(t *testing.T) {
	home := t.TempDir()
	ctx := ctxFor("linux", home, nil)

	outcome := vscodeAdapter().Install(context.Background(), BuildNPXServerSpec("ls-token", nil), ctx)
	require.Equal(t, statusInstalled, outcome.Status, outcome.Detail)

	root := readJSON(t, filepath.Join(home, ".config", "Code", "User", "mcp.json"))
	assert.NotContains(t, root, "mcpServers", "VS Code uses the top-level 'servers' key")
	entry := root["servers"].(map[string]any)["localstack"].(map[string]any)
	assert.Equal(t, "stdio", entry["type"], "VS Code entries require a type discriminator")
}

func TestFileClientInstallPreservesExistingServers(t *testing.T) {
	home := t.TempDir()
	ctx := ctxFor("darwin", home, nil)
	path := filepath.Join(home, ".cursor", "mcp.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{"other":{"command":"foo"},"localstack":{"command":"stale"}}}`), 0600))

	outcome := cursorAdapter().Install(context.Background(), BuildNPXServerSpec("ls-token", nil), ctx)
	require.Equal(t, statusInstalled, outcome.Status, outcome.Detail)
	assert.Contains(t, outcome.Detail, "updated", "re-running over an existing localstack entry reports an update")

	servers := readJSON(t, path)["mcpServers"].(map[string]any)
	assert.Contains(t, servers, "other")
	assert.Contains(t, servers, "localstack")
}

func TestFileClientDetectUnsupportedPlatform(t *testing.T) {
	installed, reason := claudeDesktopAdapter().Detect(ctxFor("linux", "/home/u", nil))
	assert.False(t, installed)
	assert.NotEmpty(t, reason)
}

// --- CLI-managed adapter ---

type fakeRunner struct {
	available map[string]bool
	calls     [][]string
	exit      int
	stderr    string
}

func (f *fakeRunner) LookPath(bin string) bool { return f.available[bin] }

func (f *fakeRunner) Run(_ context.Context, bin string, args ...string) (int, string, string, error) {
	f.calls = append(f.calls, append([]string{bin}, args...))
	return f.exit, "", f.stderr, nil
}

func TestClaudeCodeAdapterInstallRemovesThenAdds(t *testing.T) {
	runner := &fakeRunner{available: map[string]bool{"claude": true}}
	ctx := ctxFor("linux", "/home/u", nil)

	outcome := claudeCodeAdapter(runner).Install(context.Background(), BuildNPXServerSpec("ls-token", nil), ctx)
	require.Equal(t, statusInstalled, outcome.Status, outcome.Detail)

	require.Len(t, runner.calls, 2)
	assert.Equal(t, []string{"claude", "mcp", "remove", "localstack", "--scope", "user"}, runner.calls[0])
	assert.Equal(t, []string{
		"claude", "mcp", "add", "localstack", "--scope", "user",
		"--env", "LOCALSTACK_AUTH_TOKEN=ls-token",
		"--", "npx", "-y", "@localstack/localstack-mcp-server",
	}, runner.calls[1])
}

func TestCodexAdapterInstallAddsOnly(t *testing.T) {
	runner := &fakeRunner{available: map[string]bool{"codex": true}}
	outcome := codexAdapter(runner).Install(context.Background(), BuildNPXServerSpec("ls-token", nil), ctxFor("linux", "/home/u", nil))
	require.Equal(t, statusInstalled, outcome.Status)

	require.Len(t, runner.calls, 1, "codex add overwrites, no remove-first needed")
	assert.Equal(t, "add", runner.calls[0][2])
}

func TestCliAdapterFailureRedactsTokenOnly(t *testing.T) {
	runner := &fakeRunner{available: map[string]bool{"codex": true}, exit: 1, stderr: "boom: ls-secret-token failed on line 1"}
	spec := BuildNPXServerSpec("ls-secret-token", map[string]string{"DEBUG": "1"})

	outcome := codexAdapter(runner).Install(context.Background(), spec, ctxFor("linux", "/home/u", nil))
	assert.Equal(t, statusFailed, outcome.Status)
	assert.NotContains(t, outcome.Detail, "ls-secret-token", "the token must be redacted from error output")
	assert.Contains(t, outcome.Detail, "***")
	assert.Contains(t, outcome.Detail, "line 1", "short --config values must not be redacted out of the diagnostic")
}

func TestCliAdapterWrapsNpxOnWindows(t *testing.T) {
	runner := &fakeRunner{available: map[string]bool{"claude": true}}
	outcome := claudeCodeAdapter(runner).Install(context.Background(),
		BuildNPXServerSpec("ls-secret-token", nil), ctxFor("windows", `C:\Users\u`, nil))
	require.Equal(t, statusInstalled, outcome.Status, outcome.Detail)

	// calls[1] is the add (calls[0] is the remove-first). On Windows the server
	// must launch via `cmd /c npx` so the npx.cmd shim resolves.
	add := runner.calls[1]
	require.GreaterOrEqual(t, len(add), 5)
	assert.Equal(t, []string{"cmd", "/c", "npx", "-y", "@localstack/localstack-mcp-server"}, add[len(add)-5:])
}

func TestCliAdapterDetect(t *testing.T) {
	runner := &fakeRunner{available: map[string]bool{"claude": true}}
	installed, reason := claudeCodeAdapter(runner).Detect(ctxFor("linux", "/home/u", nil))
	assert.True(t, installed)
	assert.Empty(t, reason)

	installed, _ = codexAdapter(runner).Detect(ctxFor("linux", "/home/u", nil))
	assert.False(t, installed, "codex not on PATH")
}
