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
	version string
}

func NewHeader(version string) Header {
	return Header{version: version}
}

func (h Header) View() string {
	logoStyle := lipgloss.NewStyle().PaddingLeft(headerPadding)

	nimbo := logoStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		nimboLine1(),
		nimboLine2(),
		nimboLine3(),
	))

	text := lipgloss.JoinVertical(lipgloss.Left,
		styles.Title.Render("LocalStack (lstk)"),
		styles.Version.Render(h.version),
		"",
	)

	spacer := strings.Repeat(" ", headerPadding)

	return "\n" + lipgloss.JoinHorizontal(lipgloss.Top, nimbo, spacer, text) + "\n"
}
