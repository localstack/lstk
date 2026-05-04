package cmd

import (
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newStartCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "start",
		Short:   "Start emulator",
		Long:    "Start emulator and services.",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			persist, err := cmd.Flags().GetBool("persist")
			if err != nil {
				return err
			}
			return startEmulator(cmd.Context(), rt, cfg, tel, logger, persist)
		},
	}
	cmd.Flags().Bool("persist", false, "Enable local persistence (sets LOCALSTACK_PERSISTENCE=1)")
	return cmd
}
