package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newConfigCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	cmd.AddCommand(newConfigPathCmd(cfg, tel))
	return cmd
}

func newConfigPathCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path",
		RunE: commandWithTelemetry("config path", tel, func() string { return cfg.AuthToken }, func(cmd *cobra.Command, args []string) error {
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
