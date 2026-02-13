package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/auth"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with LocalStack",
	Long:  "Authenticate with LocalStack and store credentials in system keyring",
	RunE: func(cmd *cobra.Command, args []string) error {
		a, err := auth.New()
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}

		_, err = a.GetToken(cmd.Context())
		if err != nil {
			return err
		}

		fmt.Println("Login successful.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
