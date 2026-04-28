package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newSnapshotCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator snapshots",
	}
	cmd.AddCommand(newSnapshotSaveCmd(cfg, tel))
	return cmd
}

func newSnapshotSaveCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:   "save [destination]",
		Short: "Save a snapshot of the emulator state",
		Long: `Save a snapshot of the running emulator's state to a local file.

The destination must be a file path. Use a path prefix to save locally:

  lstk snapshot save                  # saves to ./ls-state-export
  lstk snapshot save ./my-snapshot    # saves to ./my-snapshot
  lstk snapshot save /tmp/my-state    # saves to /tmp/my-state

Cloud destinations are not yet supported.`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig,
		RunE: commandWithTelemetry("snapshot save", tel, func(cmd *cobra.Command, args []string) error {
			var destArg string
			if len(args) > 0 {
				destArg = args[0]
			}

			dest, err := snapshot.ParseDestination(destArg)
			if err != nil {
				return err
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}
			if len(appConfig.Containers) == 0 {
				return fmt.Errorf("no emulator configured")
			}

			c := appConfig.Containers[0]
			host, _ := endpoint.ResolveHost(c.Port, cfg.LocalStackHost)
			exporter := snapshot.NewStateClient("http://" + host)

			if isInteractiveMode(cfg) {
				return ui.RunSnapshotSave(cmd.Context(), rt, appConfig.Containers, exporter, dest)
			}
			return snapshot.Save(cmd.Context(), rt, appConfig.Containers, exporter, dest, output.NewPlainSinkSplit(os.Stdout, os.Stderr))
		}),
	}
}
