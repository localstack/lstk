package mcpconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func ctxFor(platform, home string, env map[string]string) ClientContext {
	return ClientContext{
		Platform: platform,
		HomeDir:  home,
		Getenv:   func(k string) string { return env[k] },
	}
}

func TestCursorConfigPath(t *testing.T) {
	assert.Equal(t, "/home/u/.cursor/mcp.json", cursorConfigPath(ctxFor("linux", "/home/u", nil)))
}

func TestClaudeDesktopConfigPath(t *testing.T) {
	mac, ok := claudeDesktopConfigPath(ctxFor("darwin", "/Users/u", nil))
	assert.True(t, ok)
	assert.Equal(t, "/Users/u/Library/Application Support/Claude/claude_desktop_config.json", mac)

	_, ok = claudeDesktopConfigPath(ctxFor("linux", "/home/u", nil))
	assert.False(t, ok, "Claude Desktop is unsupported on Linux")

	win, ok := claudeDesktopConfigPath(ctxFor("windows", `C:\Users\u`, map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`}))
	assert.True(t, ok)
	assert.Contains(t, win, "Claude")
}

func TestVSCodeConfigPath(t *testing.T) {
	assert.Equal(t,
		"/Users/u/Library/Application Support/Code/User/mcp.json",
		vscodeConfigPath(ctxFor("darwin", "/Users/u", nil)))

	assert.Equal(t,
		"/home/u/.config/Code/User/mcp.json",
		vscodeConfigPath(ctxFor("linux", "/home/u", nil)))

	assert.Equal(t,
		"/home/u/custom/Code/User/mcp.json",
		vscodeConfigPath(ctxFor("linux", "/home/u", map[string]string{"XDG_CONFIG_HOME": "/home/u/custom"})))
}

func TestCodexConfigPath(t *testing.T) {
	assert.Equal(t, "/home/u/.codex/config.toml", codexConfigPath(ctxFor("linux", "/home/u", nil)))
	assert.Equal(t, "/custom/config.toml", codexConfigPath(ctxFor("linux", "/home/u", map[string]string{"CODEX_HOME": "/custom"})))
}
