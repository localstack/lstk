package cmd

import (
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
	cmd := &cobra.Command{
		Use:         "status",
		Short:       "Show emulator status and deployed resources",
		Long:        "Show the status of a running emulator and its deployed resources",
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)

			clients := map[config.EmulatorType]emulator.Client{
				config.EmulatorAWS:       aws.NewClient(),
				config.EmulatorSnowflake: snowflake.NewClient(),
				config.EmulatorAzure:     azure.NewClient(),
			}

			target, err := endpoint.Resolve(cmd.Context(), cmd)
			if err != nil {
				return err
			}

			if cfg.JSON {
				includeResources, _ := cmd.Flags().GetBool("resources")
				if target != nil {
					return container.StatusExternalJSON(cmd.Context(), target, clients, sink, includeResources)
				}

				rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
				if err != nil {
					return err
				}
				appCfg, err := config.Get()
				if err != nil {
					return failGetConfig(sink, cfg, err)
				}
				return container.StatusJSON(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, clients, sink, includeResources)
			}

			if target != nil {
				if isInteractiveMode(cfg) {
					return ui.RunStatusExternal(cmd.Context(), target, clients)
				}
				return container.StatusExternal(cmd.Context(), target, clients, output.NewPlainSink(os.Stdout))
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			if isInteractiveMode(cfg) {
				return ui.RunStatus(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, clients)
			}
			return container.Status(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, clients, output.NewPlainSink(os.Stdout))
		},
	}
	cmd.Flags().Bool("resources", false, "Include deployed resource details (--json only)")
	return cmd
}
