package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newStartCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	var firstRun bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start emulator",
		Long: `Start emulator and services.

Host environment variables prefixed with LOCALSTACK_ are forwarded to the emulator.

Use --type (aws, snowflake, azure) to select the emulator non-interactively; it records the selection in config, switching the configured type in place when it differs.

If a snapshot is configured for the AWS emulator (the snapshot field in [[containers]]), it is auto-loaded once the emulator starts. Use --snapshot REF to override it for one run, or --no-snapshot to skip it.`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unexpected argument %q; select the emulator with --type (e.g. lstk start --type %s)", args[0], args[0])
			}
			return nil
		},
		PreRunE: initConfigDeferCreate(&firstRun),
		RunE: func(c *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			persist, err := c.Flags().GetBool("persist")
			if err != nil {
				return err
			}
			snapshotFlag, noSnapshot, err := snapshotFlags(c)
			if err != nil {
				return err
			}
			emulatorType, err := resolveEmulatorTypeFlag(c)
			if err != nil {
				return err
			}
			if err := applyTimeoutFlag(c, cfg); err != nil {
				return err
			}
			return startEmulator(c.Context(), rt, cfg, tel, logger, persist, firstRun, snapshotFlag, noSnapshot, emulatorType)
		},
	}
	cmd.Flags().Bool("persist", false, "Persist emulator state across restarts")
	addEmulatorTypeFlag(cmd)
	addSnapshotStartFlags(cmd)
	addTimeoutFlag(cmd)
	return cmd
}
