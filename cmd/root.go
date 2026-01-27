package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lstk",
	Short: "LocalStack CLI",
	Long:  "lstk is the command-line interface for LocalStack.",
	Run: func(c *cobra.Command, args []string) {
		runStart()
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
