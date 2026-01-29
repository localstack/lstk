package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "lstk",
	Short: "LocalStack CLI",
	Long:  "lstk is the command-line interface for LocalStack.",
	Run: func(c *cobra.Command, args []string) {
		if err := runStart(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
