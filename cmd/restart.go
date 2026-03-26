package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newRestartCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "restart",
		Short:   "Restart emulator",
		Long:    "Stop and restart the emulator, reusing the existing local image.",
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

			containers := applyContainerNameOverride(appConfig.Containers, cfg.ContainerName)

			startOpts := container.StartOptions{
				PlatformClient:   api.NewPlatformClient(cfg.APIEndpoint, logger),
				AuthToken:        cfg.AuthToken,
				ForceFileKeyring: cfg.ForceFileKeyring,
				AuthTokenFile:    cfg.AuthTokenFile,
				WebAppURL:        cfg.WebAppURL,
				LocalStackHost:   cfg.LocalStackHost,
				Containers:       containers,
				Env:              appConfig.Env,
				Logger:           logger,
				Telemetry:        tel,
			}
			stopOpts := container.StopOptions{
				Telemetry: tel,
			}

			var runErr error
			if isInteractiveMode(cfg) {
				runErr = ui.RunRestart(cmd.Context(), rt, containers, startOpts, stopOpts)
			} else {
				runErr = container.Restart(cmd.Context(), rt, output.NewPlainSink(os.Stdout), containers, startOpts, stopOpts)
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
			tel.EmitCommand(cmd.Context(), "restart", flags, time.Since(startTime).Milliseconds(), exitCode, errorMsg)

			return runErr
		},
	}
}
