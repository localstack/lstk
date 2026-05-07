package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/log"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/telemetry"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

func newLoginCmd(cfg *env.Env, tel *telemetry.Client, logger log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "login",
		Short:   "Manage login",
		Long:    "Manage login and store credentials in system keyring",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isInteractiveMode(cfg) {
				return fmt.Errorf("login requires an interactive terminal")
			}
			tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, logger)
			if err != nil {
				return fmt.Errorf("failed to initialize token storage: %w", err)
			}
			storedToken, _ := tokenStorage.GetAuthToken()
			if cfg.AuthToken != "" || storedToken != "" {
				return ui.RunMessage(cmd.Context(), output.MessageEvent{Severity: output.SeverityNote, Text: "You're already logged in"})
			}
			platformClient := api.NewPlatformClient(cfg.APIEndpoint, logger)
			if err := ui.RunLogin(cmd.Context(), version.Version(), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL, logger); err != nil {
				return err
			}
			if token, err := tokenStorage.GetAuthToken(); err == nil && token != "" {
				tel.SetAuthToken(token)
			}
			return nil
		},
	}
}
