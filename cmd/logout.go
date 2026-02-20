package cmd

import (
	"errors"
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

		if ui.IsInteractive() {
			a := auth.New(nil, platformClient, tokenStorage, false)
			return ui.RunLogout(cmd.Context(), version, a)
		}

		// Non-interactive mode: auth emits events through the sink
		sink := output.NewPlainSink(os.Stdout)
		a := auth.New(sink, platformClient, tokenStorage, false)

		err = a.Logout()
		if errors.Is(err, auth.ErrNotLoggedIn) {
			return nil
		}
		return err
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
