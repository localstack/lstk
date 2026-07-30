package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/emulator/azure"
	"github.com/localstack/lstk/internal/emulator/snowflake"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show emulator status and deployed resources",
		Long:    "Show the status of a running emulator and its deployed resources",
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := endpoint.Resolve(cmd.Context(), cmd)
			if err != nil {
				return err
			}
			if target != nil {
				return statusExternal(cmd.Context(), target)
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			clients := map[config.EmulatorType]emulator.Client{
				config.EmulatorAWS:       aws.NewClient(),
				config.EmulatorSnowflake: snowflake.NewClient(),
				config.EmulatorAzure:     azure.NewClient(),
			}

			if isInteractiveMode(cfg) {
				return ui.RunStatus(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, clients)
			}
			return container.Status(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, clients, output.NewPlainSink(os.Stdout))
		},
	}
}

// statusExternal renders status for an externally-managed endpoint
// (--endpoint-url/LSTK_ENDPOINT_URL/AWS_ENDPOINT_URL): reachability, detected
// type, reported version, and — for an AWS-typed target — deployed resources,
// without the Docker-derived facts (container uptime, bound port) that don't
// exist for an emulator lstk didn't start. Deployed resources are not
// Docker-derived (they're an ordinary emulator API call via FetchResources,
// identical to the Docker-managed path in internal/container/status.go), so
// there's no reason to omit them here. Always renders via the plain sink,
// regardless of interactive mode — there's no lifecycle/progress to show
// interactively here, just a few lines of info.
func statusExternal(ctx context.Context, target *endpoint.Target) error {
	sink := output.NewPlainSink(os.Stdout)
	host := target.URL

	clients := map[config.EmulatorType]emulator.Client{
		config.EmulatorAWS:       aws.NewClient(),
		config.EmulatorSnowflake: snowflake.NewClient(),
		config.EmulatorAzure:     azure.NewClient(),
	}

	var version string
	var rows []emulator.Resource
	if client, ok := clients[target.Type]; ok {
		if v, err := client.FetchVersion(ctx, host); err != nil {
			sink.Emit(output.MessageEvent{Severity: output.SeverityWarning, Text: fmt.Sprintf("Could not fetch version: %v", err)})
		} else {
			version = v
		}

		var fetchErr error
		rows, fetchErr = client.FetchResources(ctx, host)
		if fetchErr != nil {
			return fetchErr
		}
	}

	sink.Emit(output.InstanceInfoEvent{
		EmulatorName: target.Type.DisplayName(),
		Version:      version,
		Host:         host,
	})

	if target.Type == config.EmulatorAWS {
		if len(rows) == 0 {
			sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: "No resources deployed"})
			return nil
		}

		tableRows := make([][]string, len(rows))
		services := map[string]struct{}{}
		for i, r := range rows {
			tableRows[i] = []string{r.Service, r.Name, r.Region, r.Account}
			services[r.Service] = struct{}{}
		}

		sink.Emit(output.ResourceSummaryEvent{Resources: len(rows), Services: len(services)})
		sink.Emit(output.TableEvent{
			Headers: []string{"Service", "Resource", "Region", "Account"},
			Rows:    tableRows,
		})
	}
	return nil
}
