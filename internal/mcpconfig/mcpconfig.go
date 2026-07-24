package mcpconfig

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/localstack/lstk/internal/output"
)

// Options configures a `lstk mcp init` run. Values are resolved at the command
// boundary and passed in explicitly (no config.Get in this package).
type Options struct {
	Token     string            // LOCALSTACK_AUTH_TOKEN to embed in client configs
	Method    Method            // MethodDocker (default) or MethodNPX
	ExtraEnv  map[string]string // additional env forwarded to the server
	Docker    DockerOptions     // used when Method == MethodDocker
	ClientIDs []string          // explicit clients to configure; empty = all detected
}

// RunInit configures the selected MCP clients to launch the LocalStack MCP
// server. It emits progress through the sink and works for both the interactive
// (TUI) and non-interactive (plain) paths.
func RunInit(ctx context.Context, sink output.Sink, opts Options) error {
	cctx, err := currentClientContext()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	return runInit(ctx, sink, opts, allAdapters(execRunner{}), cctx)
}

func runInit(ctx context.Context, sink output.Sink, opts Options, adapters []ClientAdapter, cctx ClientContext) error {
	if opts.Token == "" {
		sink.Emit(output.ErrorEvent{
			Title:   "No LocalStack auth token found",
			Summary: "The MCP server needs a token to talk to LocalStack.",
			Actions: []output.ErrorAction{
				{Label: "Log in:", Value: "lstk login"},
				{Label: "Or set:", Value: "export LOCALSTACK_AUTH_TOKEN=ls-..."},
			},
		})
		return output.NewSilentError(fmt.Errorf("no auth token"))
	}

	spec, err := buildSpec(opts)
	if err != nil {
		sink.Emit(output.ErrorEvent{Title: "Invalid options", Summary: err.Error()})
		return output.NewSilentError(err)
	}

	targets, err := selectTargets(adapters, cctx, opts.ClientIDs)
	if err != nil {
		sink.Emit(output.ErrorEvent{Title: "Unknown MCP client", Summary: err.Error()})
		return output.NewSilentError(err)
	}
	if len(targets) == 0 {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     "No supported MCP clients detected. Install one (Cursor, Claude Code, Claude Desktop, VS Code, Codex), or pass --client.",
		})
		return nil
	}

	labels := make([]string, len(targets))
	for i, a := range targets {
		labels[i] = a.Label()
	}
	sink.Emit(output.MessageEvent{
		Severity: output.SeverityInfo,
		Text:     fmt.Sprintf("Configuring the LocalStack MCP server (%s) for: %s", opts.Method, strings.Join(labels, ", ")),
	})

	installed := 0
	for _, a := range targets {
		outcome := a.Install(ctx, spec, cctx)
		switch outcome.Status {
		case statusInstalled:
			installed++
			sink.Emit(output.MessageEvent{Severity: output.SeveritySuccess, Text: a.Label() + ": " + outcome.Detail})
		case statusSkipped:
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: a.Label() + ": " + outcome.Detail})
		default:
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: a.Label() + ": " + outcome.Detail})
		}
	}

	if installed == 0 {
		sink.Emit(output.ErrorEvent{Title: "No MCP clients were configured", Summary: "Every selected client failed; see the messages above."})
		return output.NewSilentError(fmt.Errorf("no clients configured"))
	}

	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySuccess,
		Text:     "Done. Restart your MCP client(s), then ask your agent to start LocalStack.",
	})
	if opts.Method == MethodDocker {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     "Docker mode mounts your Docker socket so the server can manage containers; use --method npx to avoid it.",
		})
		sink.Emit(output.MessageEvent{
			Severity: output.SeveritySecondary,
			Text:     "The first run pulls " + DockerImage + " — give it a minute.",
		})
	}
	return nil
}

func buildSpec(opts Options) (ServerSpec, error) {
	switch opts.Method {
	case MethodNPX:
		return BuildNPXServerSpec(opts.Token, opts.ExtraEnv), nil
	case MethodDocker, "":
		return BuildDockerServerSpec(opts.Token, opts.ExtraEnv, opts.Docker), nil
	default:
		return ServerSpec{}, fmt.Errorf("unknown method %q (use %q or %q)", opts.Method, MethodDocker, MethodNPX)
	}
}

// selectTargets resolves which adapters to configure. With explicit clientIDs it
// returns those adapters (erroring on unknown ids); otherwise it auto-detects
// installed, supported clients.
func selectTargets(adapters []ClientAdapter, cctx ClientContext, clientIDs []string) ([]ClientAdapter, error) {
	if len(clientIDs) > 0 {
		byID := make(map[string]ClientAdapter, len(adapters))
		for _, a := range adapters {
			byID[a.ID()] = a
		}
		var targets []ClientAdapter
		for _, id := range clientIDs {
			a, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("%q (known: %s)", id, strings.Join(adapterIDs(adapters), ", "))
			}
			targets = append(targets, a)
		}
		return targets, nil
	}

	var targets []ClientAdapter
	for _, a := range adapters {
		installed, unsupported := a.Detect(cctx)
		if unsupported == "" && installed {
			targets = append(targets, a)
		}
	}
	return targets, nil
}

func adapterIDs(adapters []ClientAdapter) []string {
	ids := make([]string, len(adapters))
	for i, a := range adapters {
		ids[i] = a.ID()
	}
	sort.Strings(ids)
	return ids
}

// SupportedClientIDs returns the configurable client identifiers, for help text.
func SupportedClientIDs() []string {
	return adapterIDs(allAdapters(execRunner{}))
}
