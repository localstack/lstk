package ui

import (
	"context"

	"github.com/localstack/lstk/internal/azureconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
)

// RunSetupAzure runs the Azure CLI setup flow with TUI output. The setup
// itself (endpoint resolution, custom cloud registration, dummy login)
// lives in azureconfig.RunSetup so non-interactive mode can reuse it.
func RunSetupAzure(parentCtx context.Context, containers []config.ContainerConfig, localStackHost, lstkConfigDir string) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return azureconfig.RunSetup(ctx, sink, containers, localStackHost, lstkConfigDir)
	})
}
