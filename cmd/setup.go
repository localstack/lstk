package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/awsconfig"
	"github.com/localstack/lstk/internal/azureconfig"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newSetupCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up emulator CLI integration",
		Long:  "Set up emulator CLI integration for AWS or Azure.",
	}
	requireSubcommand(cmd)
	cmd.AddCommand(newSetupAWSCmd(cfg))
	cmd.AddCommand(newSetupAzureCmd(cfg))
	return cmd
}

func newSetupAWSCmd(cfg *env.Env) *cobra.Command {
	c := &cobra.Command{
		Use:     "aws",
		Short:   "Set up the LocalStack AWS profile",
		Long:    "Set up the LocalStack AWS profile in ~/.aws/config and ~/.aws/credentials for use with AWS CLI and SDKs.",
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}

			if isInteractiveMode(cfg) {
				return ui.RunSetupAWS(cmd.Context(), appConfig.Containers, cfg.LocalStackHost, force)
			}

			resolvedHost, dnsOK, err := awsconfig.ResolveProfileHost(cmd.Context(), appConfig.Containers, cfg.LocalStackHost)
			if err != nil {
				return err
			}
			sink := output.NewPlainSink(os.Stdout)
			if !dnsOK {
				sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: endpoint.DNSRebindNote})
			}
			return awsconfig.SetupNonInteractive(cmd.Context(), sink, resolvedHost, force)
		},
	}
	c.Flags().Bool("force", false, "Skip the confirmation prompt and overwrite an existing localstack profile")
	return c
}

func newSetupAzureCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "azure",
		Aliases: []string{"az"},
		Short:   "Set up Azure CLI integration with LocalStack",
		Long:    "Prepare an isolated Azure CLI config directory that routes 'lstk az' commands to the LocalStack Azure emulator. Your global ~/.azure configuration is left untouched. Requires the `az` CLI and a running LocalStack Azure emulator.",
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				return fmt.Errorf("failed to resolve config directory: %w", err)
			}

			if isInteractiveMode(cfg) {
				return ui.RunSetupAzure(cmd.Context(), appConfig.Containers, cfg.LocalStackHost, configDir)
			}
			return azureconfig.RunSetup(cmd.Context(), output.NewPlainSink(os.Stdout), appConfig.Containers, cfg.LocalStackHost, configDir)
		},
	}
}
