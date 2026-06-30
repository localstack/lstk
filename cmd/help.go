package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const usageTemplate = `Usage: {{if .HasParent}}{{.UseLine}}{{else}}lstk [options] [command]{{end}}{{if not .HasParent}}

LSTK - LocalStack command-line interface{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if not .HasParent}}{{if extensions .NamePadding}}

Extensions:
{{extensions .NamePadding}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Options:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Options:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}`

const helpTemplate = `{{if not .HasParent}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{else}}{{with (or .Long .Short)}}{{wrapText . | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{end}}
`

func configureHelp(cmd *cobra.Command) {
	cobra.AddTemplateFunc("wrapText", wrapText)
	cmd.InitDefaultHelpFlag()
	cmd.Flags().Lookup("help").Usage = "Show help"
	cmd.SetUsageTemplate(usageTemplate)
	cmd.SetHelpTemplate(helpTemplate)
}

func helpWidth() int {
	const (
		maxWidth      = 100
		fallbackWidth = 80
	)
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return fallbackWidth
	}
	if w > maxWidth {
		return maxWidth
	}
	return w
}

func wrapText(s string) string {
	width := helpWidth()
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = wrapLine(line, width)
	}
	return strings.Join(lines, "\n")
}

func wrapLine(line string, width int) string {
	// Leave indented or pre-formatted lines (examples, aligned output) untouched;
	// only reflow non-indented prose that exceeds the width.
	if line == "" || line[0] == ' ' || line[0] == '\t' || len([]rune(line)) <= width {
		return line
	}
	var b strings.Builder
	lineLen := 0
	for i, word := range strings.Fields(line) {
		wordLen := len([]rune(word))
		switch {
		case i == 0:
			b.WriteString(word)
			lineLen = wordLen
		case lineLen+1+wordLen > width:
			b.WriteByte('\n')
			b.WriteString(word)
			lineLen = wordLen
		default:
			b.WriteByte(' ')
			b.WriteString(word)
			lineLen += 1 + wordLen
		}
	}
	return b.String()
}
