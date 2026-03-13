package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
)

func RenderMessage(e output.MessageEvent) string {
	return RenderWrappedMessage(e, 0)
}

func RenderWrappedMessage(e output.MessageEvent, width int) string {
	prefixText, prefix := messagePrefix(e)
	if prefixText == "" {
		style := styles.Message
		if e.Severity == output.SeveritySecondary {
			style = styles.SecondaryMessage
		}
		return style.Render(strings.Join(wrap.SoftWrap(e.Text, width), "\n"))
	}

	if width <= len([]rune(prefixText))+1 {
		return prefix + " " + styles.Message.Render(e.Text)
	}

	availableWidth := width - len([]rune(prefixText)) - 1
	lines := wrap.SoftWrap(e.Text, availableWidth)
	if len(lines) == 0 {
		return prefix
	}

	indent := strings.Repeat(" ", len([]rune(prefixText)))
	rendered := make([]string, 0, len(lines))
	rendered = append(rendered, prefix+" "+styles.Message.Render(lines[0]))
	for _, line := range lines[1:] {
		rendered = append(rendered, styles.Secondary.Render(indent)+" "+styles.Message.Render(line))
	}
	return strings.Join(rendered, "\n")
}

func messagePrefix(e output.MessageEvent) (string, string) {
	prefix := styles.Secondary.Render("> ")
	switch e.Severity {
	case output.SeveritySuccess:
		checkmark := output.SuccessMarkerText()
		return "> " + checkmark, prefix + styles.Success.Render(checkmark)
	case output.SeverityNote:
		return "> Note:", prefix + styles.Note.Render("Note:")
	case output.SeverityWarning:
		return "> Warning:", prefix + styles.Warning.Render("Warning:")
	default:
		return "", ""
	}
}
