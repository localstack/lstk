package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newSetupCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up emulator CLI integration",
		Long:  "Set up emulator CLI integration for AWS or Azure.",
	}
	cmd.AddCommand(newSetupAWSCmd(cfg))
	cmd.AddCommand(newSetupAzureCmd(cfg))
	return cmd
}

func newSetupAWSCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "aws",
		Short:   "Set up the LocalStack AWS profile",
		Long:    "Set up the LocalStack AWS profile in ~/.aws/config and ~/.aws/credentials for use with AWS CLI and SDKs.",
		PreRunE: initConfig(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			if !isInteractiveMode(cfg) {
				return fmt.Errorf("setup aws requires an interactive terminal")
			}

			return ui.RunConfigProfile(cmd.Context(), appConfig.Containers, cfg.LocalStackHost)
		},
	}
}

func newSetupAzureCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "azure",
		Short:   "Set up Azure CLI integration with LocalStack",
		Long:    "Prepare an isolated Azure CLI config directory that routes 'lstk az' commands to the LocalStack Azure emulator. Your global ~/.azure configuration is left untouched. Requires the `az` CLI and a running LocalStack Azure emulator.",
		PreRunE: initConfig(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			if !isInteractiveMode(cfg) {
				return fmt.Errorf("setup azure requires an interactive terminal")
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to resolve config directory: %w", err)
			}

			return ui.RunSetupAzure(cmd.Context(), appConfig.Containers, cfg.LocalStackHost, configDir)
		},
	}
}
