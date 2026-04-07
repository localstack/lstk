package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	cmd.AddCommand(newConfigProfileCmd(cfg, tel))
	cmd.AddCommand(newConfigPathCmd(cfg, tel))
	return cmd
}

func newConfigProfileCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "profile",
		Short:   "Deprecated: use 'lstk setup aws' instead",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("config profile", tel, func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			if !isInteractiveMode(cfg) {
				return fmt.Errorf("config profile requires an interactive terminal")
			}

			// Delegate to the same handler as "lstk setup aws"
			return ui.RunConfigProfile(cmd.Context(), appConfig.Containers, cfg.LocalStackHost)
		}),
	}
}

func newConfigPathCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path",
		RunE: commandWithTelemetry("config path", tel, func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			if path != "" {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), path)
				return err
			}

			configPath, err := config.ConfigFilePath()
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), configPath)
			return err
		}),
	}
}
