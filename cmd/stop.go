package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newStopCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:         "stop",
		Short:       "Stop emulator",
		Long:        "Stop emulator and services",
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}
			appConfig, err := config.Get()
			if err != nil {
				return failGetConfig(sink, cfg, err)
			}

			stopOpts := container.StopOptions{
				Telemetry: tel,
			}

			if isInteractiveMode(cfg) {
				return ui.RunStop(cmd.Context(), rt, appConfig.Containers, stopOpts)
			}
			return container.Stop(cmd.Context(), rt, sink, appConfig.Containers, stopOpts)
		},
	}
}
