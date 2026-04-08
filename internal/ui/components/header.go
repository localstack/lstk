package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/localstack/lstk/internal/ui/styles"
)

const headerPadding = 2

// nimbo logo lines with relative offsets for the cloud shape
func nimboLine1() string {
	return " " +
		styles.NimboDark.Render("▟") +
		styles.NimboLight.Render("████▖")
}

func nimboLine2() string {
	return styles.NimboMid.Render("▟") +
		styles.NimboLight.Render("██▙█▙█") +
		styles.NimboMid.Render("▟")
}

func nimboLine3() string {
	return "  " +
		styles.NimboDark.Render("▀▛▀▛▀")
}

type Header struct {
	version      string
	emulatorName string
	configPath   string
}

func NewHeader(version, emulatorName, configPath string) Header {
	return Header{version: version, emulatorName: emulatorName, configPath: configPath}
}

func (h Header) SetEmulatorName(name string) Header {
	h.emulatorName = name
	return h
}

func (h Header) View() string {
	logoStyle := lipgloss.NewStyle().PaddingLeft(headerPadding)

	nimbo := logoStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		nimboLine1(),
		nimboLine2(),
		nimboLine3(),
	))

	lines := []string{
		"lstk " + styles.Secondary.Render("("+h.version+")"),
		styles.Secondary.Render(h.emulatorName),
	}
	if h.configPath != "" {
		lines = append(lines, styles.Secondary.Render(h.configPath))
	}
	text := lipgloss.JoinVertical(lipgloss.Left, lines...)

	spacer := strings.Repeat(" ", headerPadding)

	joined := lipgloss.JoinHorizontal(lipgloss.Top, nimbo, spacer, text)
	joinedLines := strings.Split(joined, "\n")
	for i, line := range joinedLines {
		joinedLines[i] = strings.TrimRight(line, " ")
	}
	return "\n" + strings.Join(joinedLines, "\n") + "\n"
}
