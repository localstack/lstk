package cmd

import (
	"fmt"
	"net/http"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show emulator status and deployed resources",
		Long:    "Show the status of a running emulator and its deployed resources",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("status", tel, func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appCfg, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			awsClient := aws.NewClient(&http.Client{})

			if isInteractiveMode(cfg) {
				return ui.RunStatus(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, awsClient)
			}
			return container.Status(cmd.Context(), rt, appCfg.Containers, cfg.LocalStackHost, awsClient, output.NewPlainSink(os.Stdout))
		}),
	}
}
