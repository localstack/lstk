package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/volume"
	"github.com/spf13/cobra"
)

func newVolumeCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage emulator volume",
	}
	cmd.AddCommand(newVolumePathCmd(cfg, tel))
	cmd.AddCommand(newVolumeClearCmd(cfg, tel))
	return cmd
}

func newVolumePathCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "path",
		Short:   "Print the volume directory path",
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

func newVolumeClearCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	var force bool
	var containerName string

	cmd := &cobra.Command{
		Use:     "clear",
		Short:   "Clear emulator volume data",
		Long:    "Remove all data from the emulator volume directory. This resets cached state such as certificates, downloaded tools, and persistence data.",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("volume clear", tel, func(cmd *cobra.Command, args []string) error {
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			containers := appConfig.Containers
			if containerName != "" {
				containers, err = filterContainers(appConfig.Containers, containerName)
				if err != nil {
					return err
				}
			}

			if !isInteractiveMode(cfg) && !force {
				return fmt.Errorf("volume clear requires confirmation; use --force to skip in non-interactive mode")
			}

			if !isInteractiveMode(cfg) || force {
				sink := output.NewPlainSink(os.Stdout)
				return volume.Clear(cmd.Context(), sink, containers, true)
			}

			return ui.RunVolumeClear(cmd.Context(), containers)
		}),
	}

	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&containerName, "emulator", "", "Target emulator by type or container name")

	return cmd
}

func filterContainers(containers []config.ContainerConfig, target string) ([]config.ContainerConfig, error) {
	for _, c := range containers {
		if string(c.Type) == target || c.Name() == target {
			return []config.ContainerConfig{c}, nil
		}
	}
	var types, names []string
	for _, c := range containers {
		types = append(types, string(c.Type))
		names = append(names, c.Name())
	}
	return nil, fmt.Errorf("emulator %q not found in config; available types: %v, container names: %v", target, types, names)
}
