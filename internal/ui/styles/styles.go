package styles

import "github.com/charmbracelet/lipgloss"

const (
	NimboDarkColor  = "#3F51C7"
	NimboMidColor   = "#5E6AD2"
	NimboLightColor = "#7E88EC"
)

var (
	NimboDark = lipgloss.NewStyle().
			Foreground(lipgloss.Color(NimboDarkColor))

	NimboMid = lipgloss.NewStyle().
			Foreground(lipgloss.Color(NimboMidColor))

	NimboLight = lipgloss.NewStyle().
			Foreground(lipgloss.Color(NimboLightColor))

	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("69"))

	Version = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	Message = lipgloss.NewStyle().
		Foreground(lipgloss.Color("245"))

	// Message severity styles
	Success = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42"))

	Note = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33"))

	Warning = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	// Secondary/muted style for prefixes
	Secondary = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	// Error styles
	ErrorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196"))

	ErrorDetail = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	ErrorAction = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))

	// Spinner style
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))
)
