package azureconfig

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel"

	"github.com/localstack/lstk/internal/azurecli"
	"github.com/localstack/lstk/internal/output"
)

// PublicCloudName is the built-in Azure CLI cloud for public Azure. It is always
// registered, so it is the safe default to switch back to when stopping interception.
const PublicCloudName = "AzureCloud"

// StartInterception registers and activates the LocalStack custom cloud in the user's
// global Azure CLI config (default ~/.azure — no AZURE_CONFIG_DIR override), so plain
// `az` commands in any terminal target the LocalStack Azure emulator until
// StopInterception is run. Unlike Setup, it does not touch the isolated `lstk az` config.
// The caller is expected to have verified the Azure CLI is installed (see azPreflight).
func StartInterception(ctx context.Context, sink output.Sink, endpointURL string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.StartInterception")
	defer span.End()

	if err := IsHealthy(ctx, endpointURL); err != nil {
		return fmt.Errorf("LocalStack Azure emulator not reachable at %s — run 'lstk' to start it before running 'lstk az start-interception': %w", endpointURL, err)
	}

	// nil azEnv -> az uses the user's global ~/.azure, which is the whole point of interception.
	if err := registerLocalStackCloud(ctx, sink, nil, endpointURL, false); err != nil {
		return err
	}

	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySuccess,
		Text:     fmt.Sprintf("Interception active: all 'az' commands now target the LocalStack Azure emulator (global config, cloud '%s').", CloudName),
	})
	sink.Emit(output.MessageEvent{
		Severity: output.SeverityNote,
		Text:     "Run 'lstk az stop-interception' to switch 'az' back to real Azure.",
	})
	return nil
}

// StopInterception switches the global active Azure CLI cloud away from LocalStack back
// to targetCloud (defaulting to AzureCloud) and re-enables instance discovery. As a
// guard against clobbering an unrelated selection, it only changes the active cloud when
// LocalStack is currently active; otherwise it reports the current cloud and does nothing.
func StopInterception(ctx context.Context, sink output.Sink, targetCloud string) error {
	ctx, span := otel.Tracer("github.com/localstack/lstk/internal/azureconfig").Start(ctx, "azureconfig.StopInterception")
	defer span.End()

	if err := azurecli.CheckInstalled(); err != nil {
		sink.Emit(output.ErrorEvent{
			Title:   "az CLI not found in PATH",
			Actions: []output.ErrorAction{{Label: "Install Azure CLI:", Value: azurecli.InstallURL}},
		})
		return output.NewSilentError(err)
	}

	if targetCloud == "" {
		targetCloud = PublicCloudName
	}

	active, err := ActiveCloud(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not determine the active Azure cloud: %w", err)
	}
	if active != CloudName {
		sink.Emit(output.MessageEvent{
			Severity: output.SeverityNote,
			Text:     fmt.Sprintf("'%s' is not the active Azure cloud (currently '%s'); nothing to revert.", CloudName, active),
		})
		return nil
	}

	clouds, err := ListClouds(ctx, nil)
	if err != nil {
		return fmt.Errorf("could not list Azure clouds: %w", err)
	}
	if !slices.Contains(clouds, targetCloud) {
		return fmt.Errorf("unknown Azure cloud '%s' — available clouds: %s", targetCloud, strings.Join(clouds, ", "))
	}

	if _, _, err := azurecli.Run(ctx, nil, "cloud", "set", "--name", targetCloud, "--only-show-errors"); err != nil {
		return fmt.Errorf("could not activate '%s' cloud: %w", targetCloud, err)
	}
	// Restore the public-AAD authority validation that interception disabled.
	if _, _, err := azurecli.Run(ctx, nil, "config", "set", "core.instance_discovery=true", "--only-show-errors"); err != nil {
		return fmt.Errorf("could not configure Azure CLI: %w", err)
	}

	sink.Emit(output.MessageEvent{
		Severity: output.SeveritySuccess,
		Text:     fmt.Sprintf("Interception stopped: 'az' now targets the '%s' cloud (real Azure).", targetCloud),
	})
	return nil
}
