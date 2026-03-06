package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/localstack/lstk/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}
	cmd.AddCommand(newConfigPathCmd())
	return cmd
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, err := config.ConfigFilePath()
			if err != nil {
				return err
			}
			if outputJSON(cmd) {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"path": configPath,
				})
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), configPath)
			return err
		},
	}
}
