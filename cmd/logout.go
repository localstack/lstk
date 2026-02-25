package cmd

import (
	"fmt"
	"os"

	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/output"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Remove stored authentication credentials",
	PreRunE: initConfig,
	RunE: func(cmd *cobra.Command, args []string) error {
		sink := output.NewPlainSink(os.Stdout)
		platformClient := api.NewPlatformClient()
		tokenStorage, err := auth.NewTokenStorage()
		if err != nil {
			return fmt.Errorf("failed to initialize token storage: %w", err)
		}
		a := auth.New(sink, platformClient, tokenStorage, false)
		if err := a.Logout(); err != nil {
			return fmt.Errorf("failed to logout: %w", err)
		}
		fmt.Println("Logged out successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
