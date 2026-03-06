package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

func newLoginCmd(cfg *env.Env) *cobra.Command {
	return &cobra.Command{
		Use:     "login",
		Short:   "Manage login",
		Long:    "Manage login and store credentials in system keyring",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !useInteractiveTUI(cmd) {
				return fmt.Errorf("login requires an interactive terminal; set LOCALSTACK_AUTH_TOKEN for non-interactive use")
			}
			platformClient := api.NewPlatformClient(cfg.APIEndpoint)
			return ui.RunLogin(cmd.Context(), version.Version(), platformClient, cfg.AuthToken, cfg.ForceFileKeyring, cfg.WebAppURL)
		},
	}
}
