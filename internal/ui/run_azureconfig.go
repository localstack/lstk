package ui

import (
	"context"
	"fmt"

	"github.com/localstack/lstk/internal/azureconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
)

// RunSetupAzure runs the LocalStack Azure custom cloud setup flow with TUI output.
// It locates the Azure emulator entry in the user's config to derive the port and
// then builds the `https://azure.<host>:<port>` endpoint used by the Azure CLI.
func RunSetupAzure(parentCtx context.Context, containers []config.ContainerConfig, localStackHost string) error {
	var azureContainer *config.ContainerConfig
	for i := range containers {
		if containers[i].Type == config.EmulatorAzure {
			azureContainer = &containers[i]
			break
		}
	}
	if azureContainer == nil {
		return fmt.Errorf("no azure emulator configured in config.toml; add a [[containers]] entry with type = \"azure\"")
	}

	resolvedHost, dnsOK := endpoint.ResolveHost(parentCtx, azureContainer.Port, localStackHost)
	endpointURL := azureconfig.BuildEndpoint(resolvedHost)

	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		if !dnsOK {
			sink.Emit(output.MessageEvent{
				Severity: output.SeverityWarning,
				Text: fmt.Sprintf(
					"%s Azure setup requires DNS resolution because the LocalStack proxy routes by Host header. Configure DNS or set LOCALSTACK_HOST.",
					endpoint.DNSRebindNote,
				),
			})
			return fmt.Errorf("dns resolution required for azure setup")
		}
		return azureconfig.Setup(ctx, sink, endpointURL)
	})
}
