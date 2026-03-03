package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

func newLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "login",
		Short:   "Manage login",
		Long:    "Manage login and store credentials in system keyring",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !ui.IsInteractive() {
				return fmt.Errorf("login requires an interactive terminal")
			}
			platformClient := api.NewPlatformClient()
			return ui.RunLogin(cmd.Context(), version.Version(), platformClient)
		},
	}
}
