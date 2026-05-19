package ui

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/azureconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

// RunSetupAzure prepares the isolated Azure CLI config dir with TUI output.
// It derives the LocalStack Azure endpoint from the Azure emulator config and
// runs the setup, which registers a custom Azure cloud pointing at LocalStack
// without touching the user's global ~/.azure configuration.
func RunSetupAzure(parentCtx context.Context, containers []config.ContainerConfig, localStackHost, lstkConfigDir string) error {
	var azureContainer *config.ContainerConfig
	for i := range containers {
		if containers[i].Type == config.EmulatorAzure {
			azureContainer = &containers[i]
			break
		}
	}
	if azureContainer == nil {
		return fmt.Errorf("no azure emulator configured — run 'lstk start' and select the Azure emulator first")
	}

	resolvedHost, dnsOK := endpoint.ResolveHost(parentCtx, azureContainer.Port, localStackHost)
	endpointURL := azureconfig.BuildEndpoint(resolvedHost)
	azureConfigDir := azureconfig.ConfigDir(lstkConfigDir)

	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		if !dnsOK {
			sink.Emit(output.MessageEvent{
				Severity: output.SeverityWarning,
				Text: fmt.Sprintf(
					"%s Azure setup requires DNS resolution because LocalStack routes Azure requests by Host header. Configure DNS or set LOCALSTACK_HOST.",
					endpoint.DNSRebindNote,
				),
			})
			return fmt.Errorf("dns resolution required for azure setup")
		}
		return azureconfig.Setup(ctx, sink, endpointURL, azureConfigDir)
	})
}
