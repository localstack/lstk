package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newVolumeCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage emulator volume",
	}
	cmd.AddCommand(newVolumePathCmd(cfg, tel))
	return cmd
}

func newVolumePathCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the volume directory path",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("volume path", tel, func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			for _, c := range appConfig.Containers {
				volumeDir, err := c.VolumeDir()
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), volumeDir)
				if err != nil {
					return err
				}
			}
			return nil
		}),
	}
}
