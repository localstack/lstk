package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/emulator/aws"
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
		Long: `Save a snapshot of the running emulator's state.

Pass [destination] as an absolute or relative path for the exported file:

  lstk snapshot save                    # saves to ./snapshot-<YYYY-MM-DDTHH-mm-ss>-<hex>.zip
  lstk snapshot save ./my-snapshot.zip  # saves to ./my-snapshot.zip
  lstk snapshot save /tmp/my-state      # saves to /tmp/my-state.zip

To save to a remote pod on the LocalStack platform, use the pod: prefix:

  lstk snapshot save pod:my-baseline    # saves as a named pod on the platform`,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			var destArg string
			if len(args) > 0 {
				destArg = args[0]
			}

			dest, err := snapshot.ParseDestination(destArg, time.Now())
			if err != nil {
				return err
			}

			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			var awsContainer config.ContainerConfig
			var found bool
			for _, c := range appConfig.Containers {
				if c.Type == config.EmulatorAWS {
					awsContainer = c
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("snapshot is only supported for the AWS emulator")
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			host, _ := endpoint.ResolveHost(cmd.Context(), awsContainer.Port, cfg.LocalStackHost)
			client := aws.NewClient()
			containers := []config.ContainerConfig{awsContainer}

			if isInteractiveMode(cfg) {
				return ui.RunSnapshotSave(cmd.Context(), rt, containers, client, host, dest, cfg.AuthToken)
			}
			sink := output.NewPlainSink(os.Stdout)
			switch dest.Kind {
			case snapshot.KindPod:
				return snapshot.SavePod(cmd.Context(), rt, containers, client, host, dest.Value, cfg.AuthToken, sink)
			default:
				return snapshot.SaveLocal(cmd.Context(), rt, containers, client, host, dest.Value, sink)
			}
		},
	}
}
