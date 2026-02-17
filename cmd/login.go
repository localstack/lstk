package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with LocalStack",
	Long:  "Authenticate with LocalStack and store credentials in system keyring",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ui.IsInteractive() {
			return fmt.Errorf("login requires an interactive terminal")
		}
		platformClient := api.NewPlatformClient()
		return ui.RunLogin(cmd.Context(), version, platformClient)
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
