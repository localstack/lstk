package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/auth"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored authentication token",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := auth.New()
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}
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
