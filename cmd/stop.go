package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/spf13/cobra"
)

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "stop",
		Short:   "Stop emulator",
		Long:    "Stop emulator and services",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := runtime.NewDockerRuntime()
			if err != nil {
				return err
			}
			return container.Stop(cmd.Context(), rt, func(msg string) {
				fmt.Println(msg)
			})
		},
	}
}
