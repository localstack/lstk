package mcpconfig

import (
	"os"
	"path/filepath"
	"runtime"
)

// ClientContext carries the platform facts client path/detection logic needs,
// injectable so the resolution is unit-testable without touching the real OS.
type ClientContext struct {
	Platform string              // GOOS: "darwin", "linux", "windows"
	HomeDir  string              // user home directory
	Getenv   func(string) string // environment lookup (APPDATA, XDG_CONFIG_HOME, CODEX_HOME)
}

func currentClientContext() (ClientContext, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ClientContext{}, err
	}
	return ClientContext{
		Platform: runtime.GOOS,
		HomeDir:  home,
		Getenv:   os.Getenv,
	}, nil
}

func (c ClientContext) env(key string) string {
	if c.Getenv == nil {
		return ""
	}
	return c.Getenv(key)
}

func (c ClientContext) appData() string {
	if v := c.env("APPDATA"); v != "" {
		return v
	}
	return filepath.Join(c.HomeDir, "AppData", "Roaming")
}

func (c ClientContext) xdgConfigHome() string {
	if v := c.env("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(c.HomeDir, ".config")
}

func cursorConfigPath(ctx ClientContext) string {
	return filepath.Join(ctx.HomeDir, ".cursor", "mcp.json")
}

// claudeDesktopConfigPath returns the config path and whether the platform is
// supported (Claude Desktop ships for macOS and Windows only).
func claudeDesktopConfigPath(ctx ClientContext) (string, bool) {
	switch ctx.Platform {
	case "darwin":
		return filepath.Join(ctx.HomeDir, "Library", "Application Support", "Claude", "claude_desktop_config.json"), true
	case "windows":
		return filepath.Join(ctx.appData(), "Claude", "claude_desktop_config.json"), true
	default:
		return "", false
	}
}

func vscodeUserDir(ctx ClientContext) string {
	switch ctx.Platform {
	case "darwin":
		return filepath.Join(ctx.HomeDir, "Library", "Application Support", "Code", "User")
	case "windows":
		return filepath.Join(ctx.appData(), "Code", "User")
	default:
		return filepath.Join(ctx.xdgConfigHome(), "Code", "User")
	}
}

func vscodeConfigPath(ctx ClientContext) string {
	return filepath.Join(vscodeUserDir(ctx), "mcp.json")
}

// claudeCodeUserConfigPath is the file `claude mcp add --scope user` writes to;
// used only to describe where the entry landed.
func claudeCodeUserConfigPath(ctx ClientContext) string {
	return filepath.Join(ctx.HomeDir, ".claude.json")
}

// codexConfigPath is where `codex mcp add` persists servers; used for messaging.
func codexConfigPath(ctx ClientContext) string {
	if v := ctx.env("CODEX_HOME"); v != "" {
		return filepath.Join(v, "config.toml")
	}
	return filepath.Join(ctx.HomeDir, ".codex", "config.toml")
}
