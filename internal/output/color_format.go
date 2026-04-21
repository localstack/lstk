package output

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorErrorTitle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C33820"))
	colorErrorSecondary = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	colorErrorDetail    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	colorErrorAction    = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	colorErrorMuted     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// FormatColorEventLine is like FormatEventLine but renders ErrorEvent with ANSI color.
// All other event types delegate to FormatEventLine.
func FormatColorEventLine(event any) (string, bool) {
	if e, ok := event.(ErrorEvent); ok {
		return formatColorErrorEvent(e), true
	}
	return FormatEventLine(event)
}

func formatColorErrorEvent(e ErrorEvent) string {
	var sb strings.Builder
	sb.WriteString(colorErrorTitle.Render("✗ " + e.Title))
	if e.Summary != "" {
		sb.WriteString("\n")
		sb.WriteString(colorErrorSecondary.Render("> "))
		sb.WriteString(e.Summary)
	}
	if e.Detail != "" {
		sb.WriteString("\n  ")
		sb.WriteString(colorErrorDetail.Render(e.Detail))
	}
	if len(e.Actions) > 0 {
		sb.WriteString("\n")
		for i, action := range e.Actions {
			sb.WriteString("\n")
			if i > 0 {
				sb.WriteString(colorErrorMuted.Render(ErrorActionPrefix + action.Label + " " + action.Value))
			} else {
				sb.WriteString(colorErrorAction.Render(ErrorActionPrefix+action.Label+" "))
				sb.WriteString(lipgloss.NewStyle().Bold(true).Render(action.Value))
			}
		}
	}
	return sb.String()
}
