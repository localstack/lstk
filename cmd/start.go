package cmd

import (
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/spf13/cobra"
)

func newStartCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "start",
		Short:   "Start emulator",
		Long:    "Start emulator and services.",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime()
			if err != nil {
				return err
			}
			return runStart(cmd, rt, cfg, tel)
		},
	}
}
