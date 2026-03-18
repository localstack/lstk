package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newStopCmd(cfg *env.Env, tel *telemetry.Client) *cobra.Command {
	return &cobra.Command{
		Use:     "stop",
		Short:   "Stop emulator",
		Long:    "Stop emulator and services",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			startTime := time.Now()
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
				AuthToken: cfg.AuthToken,
			}

			var runErr error

			if isInteractiveMode(cfg) {
				runErr = ui.RunStop(cmd.Context(), rt, appConfig.Containers, stopOpts)
			} else {
				runErr = container.Stop(cmd.Context(), rt, output.NewPlainSink(os.Stdout), appConfig.Containers, stopOpts)
			}

			exitCode := 0
			errorMsg := ""
			if runErr != nil {
				exitCode = 1
				errorMsg = runErr.Error()
			}

			var flags []string
			cmd.Flags().Visit(func(f *pflag.Flag) {
				flags = append(flags, "--"+f.Name)
			})
			tel.Emit(cmd.Context(), "lstk_command", telemetry.ToMap(telemetry.CommandEvent{
				Environment: tel.GetEnvironment(cfg.AuthToken),
				Parameters:  telemetry.CommandParameters{Command: "stop", Flags: flags},
				Result: telemetry.CommandResult{
					DurationMS: time.Since(startTime).Milliseconds(),
					ExitCode:   exitCode,
					ErrorMsg:   errorMsg,
				},
			}))

			return runErr
		},
	}
}
