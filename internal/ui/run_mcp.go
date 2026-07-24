package ui

import (
	"context"

	"github.com/localstack/lstk/internal/mcpconfig"
	"github.com/localstack/lstk/internal/output"
)

// RunMCPInit runs `lstk mcp init` in the interactive TUI, streaming the
// configuration progress through a TUI sink. The command takes no interactive
// input — it just renders the domain events.
func RunMCPInit(ctx context.Context, opts mcpconfig.Options) error {
	return runWithTUI(ctx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return mcpconfig.RunInit(ctx, sink, opts)
	})
}
