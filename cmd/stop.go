package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "stop",
		Short:   "Stop emulator",
		Long:    "Stop emulator and services",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime()
			if err != nil {
				return err
			}

			if !cfg.NonInteractive && ui.IsInteractive() {
				return ui.RunStop(cmd.Context(), rt)
			}

			return container.Stop(cmd.Context(), rt, output.NewPlainSink(os.Stdout))
		},
	}
}

