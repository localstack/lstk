package cmd

import (
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

Host environment variables prefixed with LOCALSTACK_ (and the CI variable) are
forwarded to the emulator container.`,
		PreRunE: initConfig(&firstRun),
		RunE: func(c *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			persist, err := c.Flags().GetBool("persist")
			if err != nil {
				return err
			}
			return startEmulator(c.Context(), rt, cfg, tel, logger, persist, firstRun)
		},
	}
	cmd.Flags().Bool("persist", false, "Persist emulator state across restarts")
	return cmd
}
