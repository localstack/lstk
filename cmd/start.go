package cmd

import (
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:     "start",
	Short:   "Start LocalStack",
	Long:    "Start the LocalStack emulator.",
	PreRunE: initConfig,
	Run: func(cmd *cobra.Command, args []string) {
		rt, err := newRuntime(cmd.Context())
		if err != nil {
			exitWithStartError(err)
		}

		if err := runStart(cmd.Context(), rt); err != nil {
			exitWithStartError(err)
		}
	},
}
