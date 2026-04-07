package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newSetupCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up emulator CLI integration",
		Long:  "Set up emulator CLI integration (e.g., AWS, Snowflake, Azure). Currently only AWS is supported.",
	}
	cmd.AddCommand(newSetupAWSCmd(cfg, tel))
	return cmd
}

func newSetupAWSCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "aws",
		Short:   "Set up the LocalStack AWS profile",
		Long:    "Set up the LocalStack AWS profile in ~/.aws/config and ~/.aws/credentials for use with AWS CLI and SDKs.",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("setup aws", tel, func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			if !isInteractiveMode(cfg) {
				return fmt.Errorf("setup aws requires an interactive terminal")
			}

			return ui.RunConfigProfile(cmd.Context(), appConfig.Containers, cfg.LocalStackHost)
		}),
	}
}
