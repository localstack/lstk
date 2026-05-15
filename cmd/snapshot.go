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

const snapshotSaveCanonical = "snapshot save"

const snapshotSaveLong = `Save a snapshot of the running emulator's state.

Pass [destination] as an absolute or relative path for the exported file:

  lstk snapshot save                    # saves to ./snapshot-<YYYY-MM-DDTHH-mm-ss>-<hex>.zip
  lstk snapshot save ./my-snapshot.zip  # saves to ./my-snapshot.zip
  lstk snapshot save /tmp/my-state      # saves to /tmp/my-state.zip

Cloud destinations are not yet supported.`

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
		Use:     "save [destination]",
		Short:   "Save a snapshot of the emulator state",
		Long:    snapshotSaveLong,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotSave(cfg),
	}
}

func newSaveCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:         "save [destination]",
		Short:       "Save a snapshot of the emulator state",
		Long:        snapshotSaveLong,
		Args:        cobra.MaximumNArgs(1),
		PreRunE:     initConfig(nil),
		RunE:        runSnapshotSave(cfg),
		Annotations: map[string]string{canonicalCommandAnnotation: snapshotSaveCanonical},
	}
}

func runSnapshotSave(cfg *env.Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
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
		exporter := aws.NewClient()

		if isInteractiveMode(cfg) {
			return ui.RunSnapshotSave(cmd.Context(), rt, []config.ContainerConfig{awsContainer}, exporter, host, dest)
		}
		return snapshot.Save(cmd.Context(), rt, []config.ContainerConfig{awsContainer}, exporter, host, dest, output.NewPlainSink(os.Stdout))
	}
}
