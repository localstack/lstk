package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Long:  "Print version information for the lstk binary.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), versionLine())
			return err
		},
	}
}

func versionLine() string {
	return fmt.Sprintf("lstk %s (%s, %s)", version.Version(), version.Commit(), version.BuildDate())
}
