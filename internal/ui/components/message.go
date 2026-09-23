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

	// Wrap each explicit line on its own: SoftWrap splits on whitespace, so it
	// would otherwise fold a deliberate line break back into the sentence.
	// Soft-wrapped lines hang under the text; explicit ones after the marker.
	hanging := strings.Repeat(" ", len([]rune(prefixText))+1)
	var rendered []string
	for i, paragraph := range strings.Split(e.Text, "\n") {
		indent := hanging
		if i > 0 {
			indent = output.MessageLineBreakIndent
		}
		lines := []string{paragraph}
		if width > len(indent) {
			lines = wrap.SoftWrap(paragraph, width-len(indent))
		}
		for j, line := range lines {
			if i == 0 && j == 0 {
				rendered = append(rendered, prefix+" "+styles.Message.Render(line))
				continue
			}
			rendered = append(rendered, indent+styles.Message.Render(line))
		}
	}
	return strings.Join(rendered, "\n")
}

func messagePrefix(e output.MessageEvent) (string, string) {
	prefix := styles.Secondary.Render("> ")
	switch e.Severity {
	case output.SeveritySuccess:
		checkmark := output.SuccessMarker()
		return checkmark, styles.Success.Render(checkmark)
	case output.SeverityNote:
		return "> Note:", prefix + styles.Note.Render("Note:")
	case output.SeverityWarning:
		return "> Warning:", prefix + styles.Warning.Render("Warning:")
	default:
		return "", ""
	}
}
