package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lstk",
	Short: "LocalStack CLI",
	Long:  "lstk is the command-line interface for LocalStack.",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runStart(cmd.Context()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
