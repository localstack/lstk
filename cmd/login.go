package cmd

import (
	"fmt"
	"os"

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
		PreRunE: initConfigDeferCreate(nil),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenStorage, err := auth.NewTokenStorage(cfg.ForceFileKeyring, logger)
			if err != nil {
				return fmt.Errorf("failed to initialize token storage: %w", err)
			}
			// Checked before interactivity so agents, which run with a token and no
			// terminal, still get the guide instead of an error.
			storedToken, _ := tokenStorage.GetAuthToken()
			if cfg.AuthToken != "" || storedToken != "" {
				events := append([]output.MessageEvent{{Severity: output.SeverityNote, Text: "You're already logged in"}}, auth.AIAgentGuide()...)
				if isInteractiveMode(cfg) {
					return ui.RunMessage(cmd.Context(), events...)
				}
				sink := output.NewPlainSink(os.Stdout)
				for _, e := range events {
					sink.Emit(e)
				}
				return nil
			}
			if !isInteractiveMode(cfg) {
				output.NewPlainSink(os.Stdout).Emit(output.ErrorEvent{
					Title:   "login requires an interactive terminal",
					Code:    output.ErrAuthRequired,
					Actions: auth.AuthRequiredActions(),
				})
				return output.NewSilentError(fmt.Errorf("login requires an interactive terminal"))
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
