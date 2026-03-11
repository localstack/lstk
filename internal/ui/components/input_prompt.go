package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

type InputPrompt struct {
	prompt  string
	options []output.InputOption
	visible bool
}

func NewInputPrompt() InputPrompt {
	return InputPrompt{}
}

func (p InputPrompt) Show(prompt string, options []output.InputOption) InputPrompt {
	p.prompt = prompt
	p.options = options
	p.visible = true
	return p
}

func (p InputPrompt) Hide() InputPrompt {
	p.visible = false
	return p
}

func (p InputPrompt) Visible() bool {
	return p.visible
}

func (p InputPrompt) View() string {
	if !p.visible {
		return ""
	}

	lines := strings.Split(p.prompt, "\n")
	firstLine := lines[0]

	var sb strings.Builder

	// "?" prefix in secondary color
	sb.WriteString(styles.Secondary.Render("? "))

	sb.WriteString(styles.Message.Render(firstLine))

	if suffix := output.FormatPromptLabels(p.options); suffix != "" {
		sb.WriteString(styles.Secondary.Render(suffix))
	}

	if len(lines) > 1 {
		sb.WriteString("\n")
		sb.WriteString(styles.SecondaryMessage.Render(strings.Join(lines[1:], "\n")))
	}

	return sb.String()
}
