package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Remove stored authentication token",
	PreRunE: initConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		platformClient := api.NewPlatformClient()
		tokenStorage, err := auth.NewTokenStorage()
		if err != nil {
			return err
		}

		sink := output.NewPlainSink(os.Stdout)
		a := auth.New(sink, platformClient, tokenStorage, false)

		if ui.IsInteractive() {
			return ui.RunLogout(cmd.Context(), version, a)
		}

		// Non-interactive mode
		output.EmitLog(sink, "Logging out...")

		result, err := a.Logout()
		if err != nil {
			return err
		}

		if result.TokenDeleted {
			output.EmitSuccess(sink, "Logged out successfully.")
		} else {
			output.EmitNote(sink, "Not currently logged in.")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
