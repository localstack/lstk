package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	cobradoc "github.com/spf13/cobra/doc"
)

func newDocsCmd() *cobra.Command {
	var dir string
	var format string

	cmd := &cobra.Command{
		Use:    "docs",
		Short:  "Generate command documentation",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return err
			}

			switch format {
			case "man":
				header := &cobradoc.GenManHeader{
					Title:   "lstk",
					Section: "1",
					Source:  "lstk",
				}
				return cobradoc.GenManTree(cmd.Root(), header, dir)
			case "markdown":
				return cobradoc.GenMarkdownTree(cmd.Root(), dir)
			default:
				return fmt.Errorf("unsupported format: %s (use 'man' or 'markdown')", format)
			}
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "./manpages", "Output directory")
	cmd.Flags().StringVar(&format, "format", "man", "Output format (man, markdown)")

	return cmd
}
