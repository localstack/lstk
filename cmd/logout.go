package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

func newLogoutCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "logout",
		Short:   "Remove stored authentication credentials",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			platformClient := api.NewPlatformClient(cfg.APIEndpoint)
			if isInteractiveMode(cfg) {
				var rt runtime.Runtime
				if dockerRuntime, err := runtime.NewDockerRuntime(); err == nil {
					rt = dockerRuntime
				}
				return ui.RunLogout(cmd.Context(), rt, platformClient, cfg.AuthToken, cfg.ForceFileKeyring)
			}

			sink := output.NewPlainSink(os.Stdout)
			tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring)
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

			if rt, err := runtime.NewDockerRuntime(); err == nil {
				if running, err := container.AnyRunning(cmd.Context(), rt); err == nil && running {
					output.EmitNote(sink, "LocalStack is still running in the background")
				}
			}
			return nil
		},
	}
}
