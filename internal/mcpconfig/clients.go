package mcpconfig

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Install outcome statuses.
const (
	statusInstalled = "installed"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
)

// InstallOutcome is the result of configuring a single client.
type InstallOutcome struct {
	Status string // statusInstalled | statusSkipped | statusFailed
	Detail string
}

// ClientAdapter detects and configures one MCP client.
type ClientAdapter interface {
	ID() string
	Label() string
	// Detect reports whether the client is installed. A non-empty unsupported
	// reason means the client cannot be configured on this platform at all.
	Detect(cctx ClientContext) (installed bool, unsupported string)
	// Install configures the client. ctx is honored for any subprocess the
	// adapter spawns so the operation stays cancellable.
	Install(ctx context.Context, spec ServerSpec, cctx ClientContext) InstallOutcome
}

// ---- entry builders (shape of the per-server config object) ----

func standardEntry(spec ServerSpec) map[string]any {
	return map[string]any{"command": spec.Command, "args": spec.Args, "env": spec.Env}
}

// vscodeEntry uses VS Code's required "type": "stdio" discriminator.
func vscodeEntry(spec ServerSpec) map[string]any {
	return map[string]any{"type": "stdio", "command": spec.Command, "args": spec.Args, "env": spec.Env}
}

// ---- file-based client adapter ----

type fileClient struct {
	id, label   string
	rootPath    []string
	buildEntry  func(ServerSpec) map[string]any
	newFileSeed string
	// configPath returns the path and whether the platform is supported.
	configPath func(ClientContext) (string, bool)
	// detectInstalled reports presence when the platform is supported.
	detectInstalled func(ClientContext) bool
}

func (c *fileClient) ID() string    { return c.id }
func (c *fileClient) Label() string { return c.label }

func (c *fileClient) Detect(cctx ClientContext) (bool, string) {
	path, supported := c.configPath(cctx)
	if !supported || path == "" {
		return false, "not available on " + cctx.Platform
	}
	return c.detectInstalled(cctx), ""
}

// Install writes a JSON config file; it does no I/O that ctx could cancel.
func (c *fileClient) Install(_ context.Context, spec ServerSpec, cctx ClientContext) InstallOutcome {
	path, supported := c.configPath(cctx)
	if !supported || path == "" {
		return InstallOutcome{statusFailed, c.label + " is not available on this platform"}
	}

	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		// use existing content
	case errors.Is(err, os.ErrNotExist):
		existing = []byte(c.seed())
	default:
		return InstallOutcome{statusFailed, "could not read " + path + ": " + err.Error()}
	}

	verb := "added"
	if hasServerEntry(existing, c.rootPath, ServerName) {
		verb = "updated"
	}

	updated, err := applyServerEntry(existing, c.rootPath, ServerName, c.buildEntry(spec))
	if err != nil {
		return InstallOutcome{statusFailed, path + ": " + err.Error() + " — fix it manually and re-run"}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return InstallOutcome{statusFailed, "could not create " + filepath.Dir(path) + ": " + err.Error()}
	}
	// 0600: the file carries the auth token, so don't leave it world-readable.
	if err := os.WriteFile(path, updated, 0600); err != nil {
		return InstallOutcome{statusFailed, "could not write " + path + ": " + err.Error()}
	}
	return InstallOutcome{statusInstalled, verb + " " + ServerName + " in " + path}
}

func (c *fileClient) seed() string {
	if c.newFileSeed != "" {
		return c.newFileSeed
	}
	return "{}"
}

// ---- CLI-managed client adapter (claude, codex) ----

// cliRunner abstracts external CLI invocation for testability.
type cliRunner interface {
	LookPath(bin string) bool
	Run(ctx context.Context, bin string, args ...string) (exitCode int, stdout, stderr string, err error)
}

type execRunner struct{}

func (execRunner) LookPath(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}

func (execRunner) Run(ctx context.Context, bin string, args ...string) (int, string, string, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			err = nil // non-zero exit is conveyed via exitCode, not err
		}
	}
	return exitCode, outBuf.String(), errBuf.String(), err
}

type cliClient struct {
	id, label, bin string
	scope          []string // e.g. ["--scope", "user"] for claude; nil for codex
	removeFirst    bool     // claude: remove before add for idempotency
	detailPath     func(ClientContext) string
	runner         cliRunner
}

func (c *cliClient) ID() string    { return c.id }
func (c *cliClient) Label() string { return c.label }

func (c *cliClient) Detect(_ ClientContext) (bool, string) {
	return c.runner.LookPath(c.bin), ""
}

func (c *cliClient) Install(ctx context.Context, rawSpec ServerSpec, cctx ClientContext) InstallOutcome {
	// The token rides on argv (--env KEY=VALUE) because `claude/codex mcp add`
	// only persists env into the config that way — this matches the npm wizard.
	// It is briefly visible in the process table during the near-instant add;
	// the persisted config (~/.claude.json, ~/.codex/config.toml) holds it after.
	spec := rawSpec
	if cctx.Platform == "windows" {
		spec = windowsSpawnSafeSpec(rawSpec)
	}
	token := rawSpec.Env[AuthTokenEnv]

	if c.removeFirst {
		// Best effort: clears any prior entry so `add` doesn't conflict.
		removeArgs := append([]string{"mcp", "remove", ServerName}, c.scope...)
		_, _, _, _ = c.runner.Run(ctx, c.bin, removeArgs...)
	}

	args := append([]string{"mcp", "add", ServerName}, c.scope...)
	for _, k := range sortedKeys(spec.Env) {
		args = append(args, "--env", k+"="+spec.Env[k])
	}
	args = append(args, "--", spec.Command)
	args = append(args, spec.Args...)

	exitCode, _, stderr, err := c.runner.Run(ctx, c.bin, args...)
	if err != nil {
		return InstallOutcome{statusFailed, redactToken(c.bin+": "+err.Error(), token)}
	}
	if exitCode != 0 {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = c.bin + " exited non-zero"
		}
		return InstallOutcome{statusFailed, redactToken(detail, token)}
	}
	return InstallOutcome{statusInstalled, "added via `" + c.bin + " mcp add` (" + c.detailPath(cctx) + ")"}
}

// redactToken hides the auth token in CLI error output. Only the token is
// redacted (not arbitrary --config values), and only when it is long enough to
// be a real secret, so short config values don't mangle the diagnostic.
func redactToken(s, token string) string {
	if len(token) < 8 {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

// ---- registry ----

func cursorAdapter() ClientAdapter {
	return &fileClient{
		id: "cursor", label: "Cursor",
		rootPath: []string{"mcpServers"}, buildEntry: standardEntry,
		configPath:      func(ctx ClientContext) (string, bool) { return cursorConfigPath(ctx), true },
		detectInstalled: func(ctx ClientContext) bool { return dirExists(filepath.Join(ctx.HomeDir, ".cursor")) },
	}
}

func claudeDesktopAdapter() ClientAdapter {
	return &fileClient{
		id: "claude-desktop", label: "Claude Desktop",
		rootPath: []string{"mcpServers"}, buildEntry: standardEntry,
		configPath: claudeDesktopConfigPath,
		detectInstalled: func(ctx ClientContext) bool {
			path, ok := claudeDesktopConfigPath(ctx)
			return ok && dirExists(filepath.Dir(path))
		},
	}
}

func vscodeAdapter() ClientAdapter {
	return &fileClient{
		id: "vscode", label: "VS Code",
		rootPath: []string{"servers"}, buildEntry: vscodeEntry,
		configPath:      func(ctx ClientContext) (string, bool) { return vscodeConfigPath(ctx), true },
		detectInstalled: func(ctx ClientContext) bool { return dirExists(vscodeUserDir(ctx)) },
	}
}

func claudeCodeAdapter(runner cliRunner) ClientAdapter {
	return &cliClient{
		id: "claude-code", label: "Claude Code", bin: "claude",
		scope: []string{"--scope", "user"}, removeFirst: true,
		detailPath: claudeCodeUserConfigPath, runner: runner,
	}
}

func codexAdapter(runner cliRunner) ClientAdapter {
	return &cliClient{
		id: "codex", label: "Codex", bin: "codex",
		detailPath: codexConfigPath, runner: runner,
	}
}

// allAdapters returns the supported client adapters in display order.
func allAdapters(runner cliRunner) []ClientAdapter {
	return []ClientAdapter{
		cursorAdapter(),
		claudeCodeAdapter(runner),
		claudeDesktopAdapter(),
		vscodeAdapter(),
		codexAdapter(runner),
	}
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
