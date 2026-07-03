package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/awsconfig"
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

By default only snapshots you created are listed. Pass --all to include all snapshots in your organisation.

To list snapshots in your own S3 bucket, pass an s3:// location (requires a running emulator). Credentials are read from AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, from --profile, or from the profile named by AWS_PROFILE:

  lstk snapshot list s3://my-bucket/prefix
  lstk snapshot list s3://my-bucket/prefix --profile my-aws-profile`

const snapshotShowLong = `Show metadata for a cloud snapshot on the LocalStack platform.

  lstk snapshot show pod:my-baseline    # prints name, created date, size, version, services, and resource counts`

const snapshotRemoveLong = `Delete a cloud snapshot from the LocalStack platform.

Only cloud snapshots (pod: prefix) can be removed. This operation cannot be undone.

  lstk snapshot remove pod:my-baseline    # deletes the cloud snapshot named my-baseline

To skip the confirmation prompt in non-interactive mode, use --force:

  lstk snapshot remove pod:my-baseline --force`

func snapshotSaveLong(cmdName string) string {
	return fmt.Sprintf(`Save a snapshot of the running emulator's state.

Pass [destination] as an absolute or relative path for the exported file:

  lstk %[1]s                         # saves to ./snapshot-<YYYY-MM-DDTHH-mm-ss>-<hex>.snapshot
  lstk %[1]s ./my-snapshot.snapshot  # saves to ./my-snapshot.snapshot
  lstk %[1]s /tmp/my-state           # saves to /tmp/my-state.snapshot

To save to a remote pod on the LocalStack platform, use the pod: prefix:

  lstk %[1]s pod:my-baseline    # saves as a named pod on the platform

To save to your own S3 bucket, pass an s3:// location with an optional pod name (auto-generated when omitted). Credentials are read from AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, from --profile, or from the profile named by AWS_PROFILE; never put credentials in the URL.

  lstk %[1]s my-pod s3://my-bucket/prefix
  lstk %[1]s my-pod s3://my-bucket/prefix --profile my-aws-profile`, cmdName)
}

const snapshotLoadCanonical = "snapshot load"

func snapshotLoadLong(cmdName string) string {
	return fmt.Sprintf(`Load a snapshot into the running emulator, starting it first if needed.

REF identifies the snapshot to load:

  lstk %[1]s my-baseline             # loads ./my-baseline or ./my-baseline.snapshot
  lstk %[1]s ./checkpoint.snapshot   # loads from explicit path
  lstk %[1]s pod:my-baseline         # loads from LocalStack Cloud

To load from your own S3 bucket, pass the pod name and an s3:// location. Credentials are read from AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY, from --profile, or from the profile named by AWS_PROFILE:

  lstk %[1]s my-pod s3://my-bucket/prefix
  lstk %[1]s my-pod s3://my-bucket/prefix --profile my-aws-profile

Merge strategies control how snapshot state is combined with running state:

  --merge=account-region-merge  (default) snapshot wins on (service, account, region) overlap
  --merge=overwrite             wipe running state, then load
  --merge=service-merge         snapshot wins per-resource; non-overlapping resources combined`, cmdName)
}

func newSnapshotCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator snapshots",
	}
	requireSubcommand(cmd)
	cmd.AddCommand(newSnapshotSaveCmd(cfg))
	cmd.AddCommand(newSnapshotLoadCmd(cfg, tel, logger))
	cmd.AddCommand(newSnapshotListCmd(cfg, logger))
	cmd.AddCommand(newSnapshotRemoveCmd(cfg))
	cmd.AddCommand(newSnapshotShowCmd(cfg, logger))
	return cmd
}

// addSnapshotStartFlags registers the snapshot-related flags shared by the root
// and `start` commands. --snapshot overrides the configured REF for one run;
// --no-snapshot skips auto-loading for one run.
func addSnapshotStartFlags(cmd *cobra.Command) {
	cmd.Flags().String("snapshot", "", "Snapshot REF to load after start (overrides config for this run)")
	cmd.Flags().Bool("no-snapshot", false, "Skip auto-loading the configured snapshot for this run")
}

func snapshotFlags(cmd *cobra.Command) (snapshotFlag string, noSnapshot bool, err error) {
	if snapshotFlag, err = cmd.Flags().GetString("snapshot"); err != nil {
		return "", false, err
	}
	if noSnapshot, err = cmd.Flags().GetBool("no-snapshot"); err != nil {
		return "", false, err
	}
	return snapshotFlag, noSnapshot, nil
}

// resolveStartSnapshotRef resolves the snapshot REF to auto-load on start.
// Precedence: --no-snapshot disables it; otherwise --snapshot wins over the
// AWS container's configured snapshot. Returns "" when nothing should be loaded.
func resolveStartSnapshotRef(appConfig *config.Config, snapshotFlag string, noSnapshot bool) (string, error) {
	if noSnapshot && snapshotFlag != "" {
		return "", errors.New("--snapshot and --no-snapshot cannot be used together")
	}
	if noSnapshot {
		return "", nil
	}
	if snapshotFlag != "" {
		return snapshotFlag, nil
	}
	for _, c := range appConfig.Containers {
		if c.Type == config.EmulatorAWS && c.Snapshot != "" {
			return c.Snapshot, nil
		}
	}
	return "", nil
}

// newSnapshotAutoLoader returns a loader that imports the given REF into the
// running AWS emulator, or nil when ref is empty. The REF is parsed eagerly so an
// invalid value fails before the emulator starts. The loader passes a nil Starter:
// it is only invoked once the emulator is already up.
func newSnapshotAutoLoader(cfg *env.Env, rt runtime.Runtime, appConfig *config.Config, ref string) (func(context.Context, output.Sink) error, error) {
	if ref == "" {
		return nil, nil
	}

	var awsContainer config.ContainerConfig
	found := false
	for _, c := range appConfig.Containers {
		if c.Type == config.EmulatorAWS {
			awsContainer = c
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("snapshot auto-load is only supported for the AWS emulator")
	}

	home, _ := os.UserHomeDir()
	src, err := snapshot.ParseSource(ref, home)
	if err != nil {
		return nil, err
	}

	client := aws.NewClient()
	containers := []config.ContainerConfig{awsContainer}
	return func(ctx context.Context, sink output.Sink) error {
		host, _ := endpoint.ResolveHost(ctx, awsContainer.Port, cfg.LocalStackHost)
		switch src.Kind {
		case snapshot.KindPod:
			return snapshot.LoadPod(ctx, rt, containers, client, host, src.Value, cfg.AuthToken, "", nil, sink)
		default:
			return snapshot.LoadLocal(ctx, rt, containers, client, host, src.Value, "", nil, sink)
		}
	}, nil
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
		Long:    snapshotLoadLong(snapshotLoadCanonical),
		Args:    cobra.RangeArgs(1, 2),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotLoad(cfg, tel, logger),
	}
	addMergeFlag(cmd)
	addProfileFlag(cmd)
	return cmd
}

func newLoadCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "load REF",
		Short:       "Load a snapshot into the running emulator",
		Long:        snapshotLoadLong("load"),
		Args:        cobra.RangeArgs(1, 2),
		PreRunE:     initConfig(nil),
		RunE:        runSnapshotLoad(cfg, tel, logger),
		Annotations: map[string]string{canonicalCommandAnnotation: snapshotLoadCanonical},
	}
	addMergeFlag(cmd)
	addProfileFlag(cmd)
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
		profile, err := cmd.Flags().GetString("profile")
		if err != nil {
			return err
		}
		if err := snapshot.ValidateMergeStrategy(strategy); err != nil {
			return err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		podName, s3URL, isRemote, err := classifyRemoteArgs(args)
		if err != nil {
			return err
		}

		if isRemote {
			if podName == "" {
				invocation := strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
				return fmt.Errorf("a pod name is required to load from S3: lstk %s <pod-name> %s", invocation, s3URL)
			}
			if err := snapshot.ValidatePodName(podName); err != nil {
				return err
			}
			src, err := snapshot.ParseSource(s3URL, home)
			if err != nil {
				return err
			}
			creds, err := resolveS3Credentials(profile)
			if err != nil {
				return err
			}
			rt, client, host, containers, appConfig, err := resolveSnapshotDeps(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			starter := buildStarter(cfg, rt, appConfig, logger, tel)
			if isInteractiveMode(cfg) {
				return ui.RunSnapshotLoadRemoteS3(cmd.Context(), rt, containers, client, host, podName, src.Value, creds, cfg.AuthToken, strategy, starter)
			}
			sink := output.NewPlainSink(os.Stdout)
			return snapshot.LoadRemoteS3(cmd.Context(), rt, containers, client, host, podName, src.Value, creds, cfg.AuthToken, strategy, starter, sink)
		}

		src, err := snapshot.ParseSource(args[0], home)
		if err != nil {
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

	if len(appConfig.Containers) == 0 {
		return nil, nil, "", nil, nil, fmt.Errorf("no emulator is configured")
	}

	rt, err = runtime.NewDockerRuntime(cfg.DockerHost)
	if err != nil {
		return nil, nil, "", nil, nil, err
	}

	// Target the first running emulator when one is up, otherwise fall back to the
	// first configured emulator (the load command relies on this to auto-start it).
	// RunningEmulators preserves config order, so "first running" is deterministic
	// when several emulators are configured. Running-detection errors are ignored
	// here so the downstream save/load flows surface them with proper messaging.
	target := appConfig.Containers[0]
	if running, rerr := container.RunningEmulators(ctx, rt, appConfig.Containers); rerr == nil && len(running) > 0 {
		target = running[0]
	}

	host, _ = endpoint.ResolveHost(ctx, target.Port, cfg.LocalStackHost)
	return rt, aws.NewClient(), host, []config.ContainerConfig{target}, appConfig, nil
}

// addProfileFlag registers the --profile flag used to source AWS credentials for
// S3 remote snapshots.
func addProfileFlag(cmd *cobra.Command) {
	cmd.Flags().String("profile", "", "AWS profile to read S3 credentials from (defaults to AWS_* env vars, then AWS_PROFILE)")
}

// classifyRemoteArgs inspects positional args for an s3:// location. When one is
// present, it returns the S3 URL and the optional pod name (the other positional);
// ok is false when no s3:// ref is given, so the caller uses the local/pod path.
func classifyRemoteArgs(args []string) (podName, s3URL string, ok bool, err error) {
	for _, a := range args {
		if snapshot.IsS3Ref(a) {
			if s3URL != "" {
				return "", "", false, fmt.Errorf("only one s3:// location may be given")
			}
			s3URL = a
			continue
		}
		if podName != "" {
			return "", "", false, fmt.Errorf("unexpected extra argument %q", a)
		}
		podName = a
	}
	if s3URL == "" {
		return "", "", false, nil
	}
	return podName, s3URL, true, nil
}

// resolveS3Credentials reads AWS credentials for an S3 remote, following the
// AWS CLI precedence: an explicit --profile flag wins; otherwise static AWS_*
// environment variables win; otherwise the profile named by AWS_PROFILE is used.
func resolveS3Credentials(profile string) (snapshot.S3Credentials, error) {
	var (
		creds awsconfig.Credentials
		err   error
	)
	switch {
	case profile != "":
		creds, err = awsconfig.ReadProfileCredentials(profile)
		if err != nil {
			return snapshot.S3Credentials{}, err
		}
	default:
		creds, err = awsconfig.CredentialsFromEnv()
		if errors.Is(err, awsconfig.ErrNoCredentials) {
			if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
				creds, err = awsconfig.ReadProfileCredentials(envProfile)
				if err != nil {
					return snapshot.S3Credentials{}, err
				}
				break
			}
			return snapshot.S3Credentials{}, fmt.Errorf("AWS credentials required for S3 snapshots: set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, set AWS_PROFILE, or pass --profile <name>")
		}
		if err != nil {
			return snapshot.S3Credentials{}, err
		}
	}
	return snapshot.S3Credentials{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
	}, nil
}

func newSnapshotListCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list [s3://bucket/prefix]",
		Short:   "List Cloud Pod snapshots available on the LocalStack platform",
		Long:    snapshotListLong,
		Args:    cobra.MaximumNArgs(1),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotList(cfg, logger),
	}
	cmd.Flags().Bool("all", false, "List all snapshots in the organisation")
	addProfileFlag(cmd)
	return cmd
}

func runSnapshotList(cfg *env.Env, logger log.Logger) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		all, err := cmd.Flags().GetBool("all")
		if err != nil {
			return err
		}
		profile, err := cmd.Flags().GetString("profile")
		if err != nil {
			return err
		}

		if len(args) == 1 && snapshot.IsS3Ref(args[0]) {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			src, err := snapshot.ParseSource(args[0], home)
			if err != nil {
				return err
			}
			creds, err := resolveS3Credentials(profile)
			if err != nil {
				return err
			}
			rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if isInteractiveMode(cfg) {
				return ui.RunSnapshotListRemoteS3(cmd.Context(), rt, containers, client, host, src.Value, creds, cfg.AuthToken)
			}
			sink := output.NewPlainSink(os.Stdout)
			return snapshot.ListRemoteS3(cmd.Context(), rt, containers, client, host, src.Value, creds, cfg.AuthToken, sink)
		}
		if len(args) == 1 {
			return fmt.Errorf("unexpected argument %q: snapshot list takes an optional s3:// location", args[0])
		}

		creator := "me"
		if all {
			creator = ""
		}
		client := api.NewPlatformClient(cfg.APIEndpoint, logger)
		if isInteractiveMode(cfg) {
			return ui.RunSnapshotList(cmd.Context(), client, cfg.AuthToken, creator)
		}
		sink := output.NewPlainSink(os.Stdout)
		return snapshot.List(cmd.Context(), client, cfg.AuthToken, creator, sink)
	}
}

func newSnapshotShowCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "show REF",
		Short:   "Show metadata for a cloud snapshot",
		Long:    snapshotShowLong,
		Args:    cobra.ExactArgs(1),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotShow(cfg, logger),
	}
}

func runSnapshotShow(cfg *env.Env, logger log.Logger) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		ref, err := snapshot.ParseShowable(args[0], cwd, home)
		if err != nil {
			return err
		}

		client := api.NewPlatformClient(cfg.APIEndpoint, logger)
		if isInteractiveMode(cfg) {
			return ui.RunSnapshotShow(cmd.Context(), client, cfg.AuthToken, ref.Value)
		}
		sink := output.NewPlainSink(os.Stdout)
		return snapshot.Show(cmd.Context(), client, cfg.AuthToken, ref.Value, sink)
	}
}

func newSnapshotSaveCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "save [destination]",
		Short:   "Save a snapshot of the emulator state",
		Long:    snapshotSaveLong(snapshotSaveCanonical),
		Args:    cobra.MaximumNArgs(2),
		PreRunE: initConfig(nil),
		RunE:    runSnapshotSave(cfg),
	}
	addProfileFlag(cmd)
	return cmd
}

func newSaveCmd(cfg *env.Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:         "save [destination]",
		Short:       "Save a snapshot of the emulator state",
		Long:        snapshotSaveLong("save"),
		Args:        cobra.MaximumNArgs(2),
		PreRunE:     initConfig(nil),
		RunE:        runSnapshotSave(cfg),
		Annotations: map[string]string{canonicalCommandAnnotation: snapshotSaveCanonical},
	}
	addProfileFlag(cmd)
	return cmd
}

func runSnapshotSave(cfg *env.Env) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		profile, err := cmd.Flags().GetString("profile")
		if err != nil {
			return err
		}

		podName, s3URL, isRemote, err := classifyRemoteArgs(args)
		if err != nil {
			return err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}

		if isRemote {
			dest, err := snapshot.ParseDestination(s3URL, home, time.Now())
			if err != nil {
				return err
			}
			if podName == "" {
				podName = snapshot.DefaultRemotePodName(time.Now())
			} else if err := snapshot.ValidatePodName(podName); err != nil {
				return err
			}
			creds, err := resolveS3Credentials(profile)
			if err != nil {
				return err
			}
			rt, client, host, containers, _, err := resolveSnapshotDeps(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			if isInteractiveMode(cfg) {
				return ui.RunSnapshotSaveRemoteS3(cmd.Context(), rt, containers, client, host, podName, dest.Value, creds, cfg.AuthToken)
			}
			sink := output.NewPlainSink(os.Stdout)
			return snapshot.SaveRemoteS3(cmd.Context(), rt, containers, client, host, podName, dest.Value, creds, cfg.AuthToken, sink)
		}

		var destArg string
		if len(args) > 0 {
			destArg = args[0]
		}
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
