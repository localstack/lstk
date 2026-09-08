package cmd

import (
	"os"

	"github.com/localstack/lstk/internal/env"
	"github.com/localstack/lstk/internal/ui"
	"github.com/localstack/lstk/internal/update"
	"github.com/spf13/cobra"
)

func newUpdateCmd(cfg *env.Env) *cobra.Command {
	var checkOnly bool
	var force bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update lstk to the latest version",
		Long: "Check for and apply updates to the lstk CLI. Respects the original installation method (Homebrew, npm, or direct binary).\n\n" +
			"An install managed by an external tool (mise, nix, guix, asdf, scoop, chocolatey), or one in a directory lstk cannot write to, is not updated in place — update it through that tool instead, or pass --force to replace the binary anyway. --check always reports whether a newer version exists, whatever the install.\n\n" +
			"This command always checks for updates. The [cli] update_check config key and LSTK_UPDATE_CHECK only govern the automatic check on 'lstk start'.",
		PreRunE:     initConfigDeferCreate(nil),
		Annotations: map[string]string{jsonSupportedAnnotation: "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			sink := jsonAwareSink(cmd, cfg, os.Stdout)

			if isInteractiveMode(cfg) {
				return ui.RunUpdate(cmd.Context(), checkOnly, cfg.GitHubToken, force)
			}
			return update.Update(cmd.Context(), sink, checkOnly, cfg.GitHubToken, force)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "Only check for updates without applying them")
	// Detection is a path-marker heuristic that cannot know every packaging
	// layout, so a user it misreads needs a way through.
	cmd.Flags().BoolVar(&force, "force", false, "Replace the binary even when the install is externally managed or its directory looks unwritable")

	return cmd
}
