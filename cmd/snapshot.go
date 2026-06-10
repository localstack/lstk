package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/emulator/aws"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/snapshot"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

const snapshotSaveCanonical = "snapshot save"

const snapshotListLong = `List Cloud Pod snapshots available on the LocalStack platform.

By default only snapshots you created are listed. Pass --all to include all snapshots in your organisation.`

const snapshotRemoveLong = `Delete a cloud snapshot from the LocalStack platform.

Only cloud snapshots (pod: prefix) can be removed. This operation cannot be undone.

  lstk snapshot remove pod:my-baseline    # deletes the cloud snapshot named my-baseline

To skip the confirmation prompt in non-interactive mode, use --force:

  lstk snapshot remove pod:my-baseline --force`

const snapshotSaveLong = `Save a snapshot of the running emulator's state.

Pass [destination] as an absolute or relative path for the exported file:

  lstk snapshot save                    # saves to ./snapshot-<YYYY-MM-DDTHH-mm-ss>-<hex>.zip
  lstk snapshot save ./my-snapshot.zip  # saves to ./my-snapshot.zip
  lstk snapshot save /tmp/my-state      # saves to /tmp/my-state.zip

To save to a remote pod on the LocalStack platform, use the pod: prefix:

  lstk snapshot save pod:my-baseline    # saves as a named pod on the platform`

const snapshotLoadCanonical = "snapshot load"

const snapshotLoadLong = `Load a snapshot into the running emulator, starting it first if needed.

REF identifies the snapshot to load:

  lstk snapshot load my-baseline           # loads ./my-baseline or ./my-baseline.zip
  lstk snapshot load ./checkpoint.zip      # loads from explicit path
  lstk snapshot load pod:my-baseline       # loads from LocalStack Cloud

Merge strategies control how snapshot state is combined with running state:

  --merge=account-region-merge  (default) snapshot wins on (service, account, region) overlap
  --merge=overwrite             wipe running state, then load
  --merge=service-merge         snapshot wins per-resource; non-overlapping resources combined`

func newSnapshotCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator snapshots",
	}
	cmd.AddCommand(newSnapshotSaveCmd(cfg))
	cmd.AddCommand(newSnapshotLoadCmd(cfg, tel, logger))
	cmd.AddCommand(newSnapshotListCmd(cfg))
	cmd.AddCommand(newSnapshotRemoveCmd(cfg))
	return cmd
}

func buildStarter(cfg *env.Env, rt runtime.Runtime, appConfig *config.Config, logger log.Logger, tel *telemetry.Client) snapshot.Starter {
	return func(ctx context.Context, sink output.Sink) error {
		opts := buildStartOptions(cfg, appConfig, logger, tel, false)
		_, err := container.Start(ctx, rt, sink, opts, false)
		return err
	}
}

func newSnapshotLoadCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "load REF",
		Short:   "Load a snapshot into the running emulator",
		Long:    snapshotLoadLong,
		Args:    cobra.ExactArgs(1),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotLoad(cfg, tel, logger),
	}
	addMergeFlag(cmd)
	return cmd
}

func newLoadCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "load REF",
		Short:       "Load a snapshot into the running emulator",
		Long:        snapshotLoadLong,
		Args:        cobra.ExactArgs(1),
		PreRunE:     initConfig(nil),
		RunE:        runSnapshotLoad(cfg, tel, logger),
		Annotations: map[string]string{canonicalCommandAnnotation: snapshotLoadCanonical},
	}
	addMergeFlag(cmd)
	return cmd
}

func addMergeFlag(cmd *cobra.Command) {
	cmd.Flags().String("merge", snapshot.MergeStrategyAccountRegion, "Merge strategy: overwrite, account-region-merge, service-merge")
}

func runSnapshotLoad(cfg *env.Env, tel *telemetry.Client, logger log.Logger) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		strategy, err := cmd.Flags().GetString("merge")
		if err != nil {
			return err
		}

		home, _ := os.UserHomeDir()
		src, err := snapshot.ParseSource(args[0], home)
		if err != nil {
			return err
		}

		if err := snapshot.ValidateMergeStrategy(strategy); err != nil {
			return err
		}

		rt, client, host, containers, appConfig, err := resolveSnapshotDeps(cmd.Context(), cfg)
		if err != nil {
			return err
		}

		starter := buildStarter(cfg, rt, appConfig, logger, tel)

		if isInteractiveMode(cfg) {
			return ui.RunSnapshotLoad(cmd.Context(), rt, containers, client, host, src, cfg.AuthToken, strategy, starter)
		}
		sink := output.NewPlainSink(os.Stdout)
		switch src.Kind {
		case snapshot.KindPod:
			return snapshot.LoadPod(cmd.Context(), rt, containers, client, host, src.Value, cfg.AuthToken, strategy, starter, sink)
		default:
			return snapshot.LoadLocal(cmd.Context(), rt, containers, client, host, src.Value, strategy, starter, sink)
		}
	}
}

func newSnapshotRemoveCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove REF",
		Short:   "Delete a cloud snapshot from the LocalStack platform",
		Long:    snapshotRemoveLong,
		Args:    cobra.ExactArgs(1),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotRemove(cfg),
	}
	cmd.Flags().Bool("force", false, "Skip confirmation prompt")
	return cmd
}

func runSnapshotRemove(cfg *env.Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			return err
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		if !isInteractiveMode(cfg) {
			ref, err := snapshot.ParseRemovable(args[0], cwd, home)
			if err != nil {
				return err
			}
			if !force {
				return fmt.Errorf("snapshot remove requires confirmation; use --force to skip in non-interactive mode")
			}
			rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			sink := output.NewPlainSink(os.Stdout)
			return snapshot.Remove(cmd.Context(), rt, containers, ref.Value, cfg.AuthToken, client, host, force, sink)
		}

		rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		return ui.RunSnapshotRemove(cmd.Context(), rt, containers, client, host, args[0], cwd, home, cfg.AuthToken, force)
	}
}

func resolveSnapshotDeps(ctx context.Context, cfg *env.Env) (rt runtime.Runtime, client *aws.Client, host string, containers []config.ContainerConfig, appConfig *config.Config, err error) {
	appConfig, err = config.Get()
	if err != nil {
		return nil, nil, "", nil, nil, fmt.Errorf("failed to get config: %w", err)
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
		return nil, nil, "", nil, nil, fmt.Errorf("snapshot is only supported for the AWS emulator")
	}

	rt, err = runtime.NewDockerRuntime(cfg.DockerHost)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}
	host, _ = endpoint.ResolveHost(ctx, awsContainer.Port, cfg.LocalStackHost)
	return rt, aws.NewClient(), host, []config.ContainerConfig{awsContainer}, appConfig, nil
}

func newSnapshotListCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List Cloud Pod snapshots available on the LocalStack platform",
		Long:    snapshotListLong,
		Args:    cobra.NoArgs,
		PreRunE: initConfig(nil),
		RunE:    runSnapshotList(cfg),
	}
	cmd.Flags().Bool("all", false, "List all snapshots in the organisation")
	return cmd
}

func runSnapshotList(cfg *env.Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		all, err := cmd.Flags().GetBool("all")
		if err != nil {
			return err
		}
		creator := "me"
		if all {
			creator = ""
		}
		rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		if isInteractiveMode(cfg) {
			return ui.RunSnapshotList(cmd.Context(), rt, containers, client, host, cfg.AuthToken, creator)
		}
		sink := output.NewPlainSink(os.Stdout)
		return snapshot.List(cmd.Context(), rt, containers, client, host, cfg.AuthToken, creator, sink)
	}
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

		home, _ := os.UserHomeDir()
		dest, err := snapshot.ParseDestination(destArg, home, time.Now())
		if err != nil {
			return err
		}

		rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
		if err != nil {
			return err
		}

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
	}
}
