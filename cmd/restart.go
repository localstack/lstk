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
	cmd := &cobra.Command{
		Use:     "restart",
		Short:   "Restart emulator",
		Long:    "Stop and restart emulator and services.\n\nAn emulator started with --image restarts from the same image. Pass --image to restart from a different one.",
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := rejectEndpointURL(cmd, output.NewPlainSink(os.Stdout), "restart"); err != nil {
				return err
			}

			rt, err := runtime.NewDockerRuntime(cfg.DockerHost)
			if err != nil {
				return err
			}

			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}

			persist, err := cmd.Flags().GetBool("persist")
			if err != nil {
				return err
			}

			stopOpts := container.StopOptions{
				Telemetry: tel,
			}
			imageOverride, err := resolveImageFlag(cmd)
			if err != nil {
				return err
			}
			if imageOverride != "" && len(appConfig.Containers) == 1 {
				if err := container.RejectImageOfOtherType(output.NewPlainSink(os.Stdout), imageOverride, appConfig.Containers[0].Type); err != nil {
					return err
				}
			}

			startOpts := buildStartOptions(cfg, appConfig, logger, tel, persist, false, imageOverride)

			if isInteractiveMode(cfg) {
				return ui.RunRestart(cmd.Context(), rt, stopOpts, startOpts)
			}

			sink := output.NewPlainSink(os.Stdout)
			return container.Restart(cmd.Context(), rt, sink, stopOpts, startOpts, false)
		},
	}
	cmd.Flags().Bool("persist", false, "Persist emulator state across restarts")
	addImageFlag(cmd)
	return cmd
}
