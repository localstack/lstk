package styles

import (
	"github.com/charmbracelet/lipgloss"
)

const (
	NimboDarkColor  = "#3F51C7"
	NimboMidColor   = "#5E6AD2"
	NimboLightColor = "#7E88EC"
	SuccessColor    = "#B7C95C"
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

	Message = lipgloss.NewStyle()

	SecondaryMessage = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	Highlight = lipgloss.NewStyle().
			Foreground(lipgloss.Color(NimboLightColor))

	Link = lipgloss.NewStyle().
		Foreground(lipgloss.Color(NimboLightColor)).
		Underline(true)

	// Message severity styles
	Success = lipgloss.NewStyle().
		Foreground(lipgloss.Color(SuccessColor))

	Note = lipgloss.NewStyle().
		Foreground(lipgloss.Color("33"))

	Warning = lipgloss.NewStyle().
		Foreground(lipgloss.Color("214"))

	LogError = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1"))

	// Secondary/muted style for prefixes
	Secondary = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	// Error styles
	ErrorTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("1"))

	ErrorDetail = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	ErrorAction = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))

	// Spinner style
	SpinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("69"))
)
