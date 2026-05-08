package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newSnapshotCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator snapshots",
	}
	cmd.AddCommand(newSnapshotSaveCmd(cfg))
	return cmd
}

func newSnapshotSaveCmd(cfg *env.Env) *cobra.Command {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			var destArg string
			if len(args) > 0 {
				destArg = args[0]
			}

			dest, err := snapshot.ParseDestination(destArg)
			if err != nil {
				return err
			}

			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			hasAWS := slices.ContainsFunc(appConfig.Containers, func(c config.ContainerConfig) bool {
				return c.Type == config.EmulatorAWS
			})
			hasOther := slices.ContainsFunc(appConfig.Containers, func(c config.ContainerConfig) bool {
				return c.Type != config.EmulatorAWS
			})
			if !hasAWS && hasOther {
				return fmt.Errorf("snapshot is only supported for the AWS emulator")
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			awsContainer := config.ContainerConfig{Type: config.EmulatorAWS, Port: config.DefaultAWSPort}
			host, _ := endpoint.ResolveHost(awsContainer.Port, cfg.LocalStackHost)
			exporter := snapshot.NewStateClient("http://" + host)

			containers := []config.ContainerConfig{awsContainer}
			if isInteractiveMode(cfg) {
				return ui.RunSnapshotSave(cmd.Context(), rt, containers, exporter, dest)
			}
			return snapshot.Save(cmd.Context(), rt, containers, exporter, dest, output.NewPlainSinkSplit(os.Stdout, os.Stderr))
		},
	}
}
