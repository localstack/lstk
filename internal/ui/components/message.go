package components

import (
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

func RenderMessage(e output.MessageEvent) string {
	prefix := styles.Secondary.Render("> ")
	switch e.Severity {
	case output.SeveritySuccess:
		return prefix + styles.Success.Render("Success:") + " " + styles.Message.Render(e.Text)
	case output.SeverityNote:
		return prefix + styles.Note.Render("Note:") + " " + styles.Message.Render(e.Text)
	case output.SeverityWarning:
		return prefix + styles.Warning.Render("Warning:") + " " + styles.Message.Render(e.Text)
	default:
		return styles.Message.Render(e.Text)
	}
}
