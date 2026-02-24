package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the lstk version",
	Long:  "Print version information for the lstk binary.",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), versionLine())
		return err
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func versionLine() string {
	return fmt.Sprintf("lstk %s (%s, %s)", version.Version(), version.Commit(), version.BuildDate())
}
