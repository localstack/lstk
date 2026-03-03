package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Remove stored authentication credentials",
	PreRunE: initConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		if ui.IsInteractive() {
			return ui.RunLogout(cmd.Context())
		}

		sink := output.NewPlainSink(os.Stdout)
		platformClient := api.NewPlatformClient()
		tokenStorage, err := auth.NewTokenStorage()
		if err != nil {
			return fmt.Errorf("failed to initialize token storage: %w", err)
		}
		a := auth.New(sink, platformClient, tokenStorage, false)
		if err := a.Logout(); err != nil {
			if errors.Is(err, auth.ErrNotLoggedIn) {
				return nil
			}
			return fmt.Errorf("failed to logout: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
