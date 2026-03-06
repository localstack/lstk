package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type commandDesc struct {
	Name     string        `json:"name"`
	Path     string        `json:"path"`
	Short    string        `json:"short,omitempty"`
	Long     string        `json:"long,omitempty"`
	Flags    []flagDesc    `json:"flags,omitempty"`
	Commands []commandDesc `json:"commands,omitempty"`
}

type flagDesc struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Usage     string `json:"usage"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
}

func newDescribeCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "describe",
		Short: "Describe CLI commands and flags as JSON",
		Long:  "Print a machine-readable JSON description of all available commands and their flags.",
		RunE: func(cmd *cobra.Command, args []string) error {
			desc := describeCommand(root)
			out, err := json.MarshalIndent(desc, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(out))
			return err
		},
	}
}

func describeCommand(cmd *cobra.Command) commandDesc {
	desc := commandDesc{
		Name:  cmd.Name(),
		Path:  cmd.CommandPath(),
		Short: cmd.Short,
		Long:  cmd.Long,
	}

	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		desc.Flags = append(desc.Flags, flagDesc{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Usage:     f.Usage,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
		})
	})

	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() && !isInternalCommand(sub.Name()) {
			desc.Commands = append(desc.Commands, describeCommand(sub))
		}
	}

	return desc
}

func isInternalCommand(name string) bool {
	switch name {
	case "describe", "completion", "help":
		return true
	default:
		return false
	}
}
