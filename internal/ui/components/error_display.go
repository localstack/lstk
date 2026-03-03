package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
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
		lines := softWrap(e.event.Summary, summaryWidth)
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

// softWrap breaks text into lines at word boundaries, falling back to hard
// breaks for words longer than maxWidth.
func softWrap(text string, maxWidth int) []string {
	if maxWidth <= 0 || len(text) <= maxWidth {
		return []string{text}
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	var lines []string
	var current strings.Builder

	for _, word := range words {
		if current.Len() == 0 {
			current.WriteString(word)
			continue
		}
		if current.Len()+1+len(word) > maxWidth {
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
		} else {
			current.WriteByte(' ')
			current.WriteString(word)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}

	return lines
}
