package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newLogoutCmd(cfg *env.Env, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Remove stored authentication credentials",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			platformClient := api.NewPlatformClient(cfg.APIEndpoint)
			appConfig, err := config.Get()
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}
			var rt runtime.Runtime
			if dockerRuntime, err := runtime.NewDockerRuntime(); err == nil {
				rt = dockerRuntime
			}

			if isInteractiveMode(cfg) {
				return ui.RunLogout(cmd.Context(), rt, platformClient, cfg.AuthToken, cfg.ForceFileKeyring, appConfig.Containers, logger)
			}

			sink := output.NewPlainSink(os.Stdout)
			tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, logger)
			if err != nil {
				return fmt.Errorf("failed to initialize token storage: %w", err)
			}
			a := auth.New(sink, platformClient, tokenStorage, cfg.AuthToken, "", false)
			if err := a.Logout(); err != nil {
				if errors.Is(err, auth.ErrNotLoggedIn) {
					return nil
				}
				return fmt.Errorf("failed to logout: %w", err)
			}

			if rt != nil {
				if running, err := container.AnyRunning(cmd.Context(), rt, appConfig.Containers); err == nil && running {
					output.EmitNote(sink, "LocalStack is still running in the background")
				}
			}
			return nil
		},
	}
}
