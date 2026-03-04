package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
)

type ErrorDisplay struct {
	event   *output.ErrorEvent
	visible bool
}

func NewErrorDisplay() ErrorDisplay {
	return ErrorDisplay{}
}

func (e ErrorDisplay) Show(event output.ErrorEvent) ErrorDisplay {
	e.event = &event
	e.visible = true
	return e
}

func (e ErrorDisplay) Visible() bool {
	return e.visible
}

func (e ErrorDisplay) View(maxWidth int) string {
	if !e.visible || e.event == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(styles.ErrorTitle.Render("✗ " + e.event.Title))
	sb.WriteString("\n")

	if e.event.Summary != "" {
		prefix := "> "
		summaryWidth := maxWidth - len(prefix)
		lines := wrap.SoftWrap(e.event.Summary, summaryWidth)
		for i, line := range lines {
			if i == 0 {
				sb.WriteString(styles.Secondary.Render(prefix))
			} else {
				sb.WriteString(strings.Repeat(" ", len(prefix)))
			}
			sb.WriteString(styles.Message.Render(line))
			sb.WriteString("\n")
		}
	}

	if e.event.Detail != "" {
		sb.WriteString("  ")
		sb.WriteString(styles.ErrorDetail.Render(e.event.Detail))
		sb.WriteString("\n")
	}

	if len(e.event.Actions) > 0 {
		sb.WriteString("\n")
		for i, action := range e.event.Actions {
			if i > 0 {
				sb.WriteString(styles.SecondaryMessage.Render("⇒ " + action.Label + " " + action.Value))
			} else {
				sb.WriteString(styles.ErrorAction.Render("⇒ " + action.Label + " "))
				sb.WriteString(styles.Message.Bold(true).Render(action.Value))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

