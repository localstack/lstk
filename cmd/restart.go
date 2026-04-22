package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newRestartCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "restart",
		Short:   "Restart emulator",
		Long:    "Stop and restart emulator and services.",
		PreRunE: initConfig,
		RunE: commandWithTelemetry("restart", tel, func(cmd *cobra.Command, args []string) error {
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
			startOpts := buildStartOptions(cfg, appConfig, logger, tel)

			if isInteractiveMode(cfg) {
				return ui.RunRestart(cmd.Context(), rt, stopOpts, startOpts)
			}

			sink := output.NewPlainSink(os.Stdout)
			return container.Restart(cmd.Context(), rt, sink, stopOpts, startOpts, false)
		}),
	}
}
