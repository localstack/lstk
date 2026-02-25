package cmd

import (
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the configuration file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := config.ConfigFilePath()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), configPath)
		return err
	},
}

func init() {
	configCmd.AddCommand(configPathCmd)
	rootCmd.AddCommand(configCmd)
}
