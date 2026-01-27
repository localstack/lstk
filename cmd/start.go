package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start LocalStack",
	Long:  "Start the LocalStack emulator.",
	Run: func(cmd *cobra.Command, args []string) {
		runStart()
	},
}

func runStart() {
	fmt.Println("Hello from LocalStack!")
}
