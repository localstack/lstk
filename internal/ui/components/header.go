package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/localstack/lstk/internal/ui/styles"
)

const headerPadding = 3

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
	endpoint     string
}

func NewHeader(version, emulatorName, endpoint string) Header {
	return Header{version: version, emulatorName: emulatorName, endpoint: endpoint}
}

func (h Header) View() string {
	logoStyle := lipgloss.NewStyle().PaddingLeft(headerPadding)

	nimbo := logoStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		nimboLine1(),
		nimboLine2(),
		nimboLine3(),
	))

	text := lipgloss.JoinVertical(lipgloss.Left,
		"lstk " + styles.Secondary.Render("("+h.version+")"),
		styles.Secondary.Render(h.emulatorName),
		styles.Secondary.Render(h.endpoint),
	)

	spacer := strings.Repeat(" ", headerPadding)

	joined := lipgloss.JoinHorizontal(lipgloss.Top, nimbo, spacer, text)
	lines := strings.Split(joined, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return "\n" + strings.Join(lines, "\n") + "\n"
}
