package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(cfg *env.Env) *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Update lstk to the latest version",
		Long:    "Check for and apply updates to the lstk CLI. Respects the original installation method (Homebrew, npm, or direct binary).",
		PreRunE: initConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			if isInteractiveMode(cfg) {
				return ui.RunUpdate(cmd.Context(), checkOnly, cfg.GitHubToken)
			}
			return update.Update(cmd.Context(), output.NewPlainSink(os.Stdout), checkOnly, cfg.GitHubToken)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates without applying them")

	return cmd
}
