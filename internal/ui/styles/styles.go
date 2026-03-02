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

	Message = lipgloss.NewStyle()

	SecondaryMessage = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	Highlight = lipgloss.NewStyle().
			Foreground(lipgloss.Color(NimboLightColor))

	Link = lipgloss.NewStyle().
		Foreground(lipgloss.Color(NimboLightColor)).
		Underline(true)
)
