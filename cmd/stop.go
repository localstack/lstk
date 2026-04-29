package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "stop",
		Short:   "Stop emulator",
		Long:    "Stop emulator and services",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			stopOpts := container.StopOptions{
				Telemetry: tel,
			}

			if isInteractiveMode(cfg) {
				return ui.RunStop(cmd.Context(), rt, appConfig.Containers, stopOpts)
			}
			return container.Stop(cmd.Context(), rt, output.NewPlainSink(os.Stdout), appConfig.Containers, stopOpts)
		},
	}
}
