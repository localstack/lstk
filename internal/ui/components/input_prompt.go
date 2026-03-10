package components

import (
	"fmt"
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

	// Style trailing "?" with secondary color
	if before, found := strings.CutSuffix(firstLine, "?"); found {
		sb.WriteString(styles.Message.Render(before + "?"))
	} else {
		sb.WriteString(styles.Message.Render(firstLine))
	}

	// Style option labels with secondary color
	labels := make([]string, 0, len(p.options))
	for _, opt := range p.options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
	}
	if len(labels) == 1 {
		sb.WriteString(styles.Secondary.Render(fmt.Sprintf(" (%s)", labels[0])))
	} else if len(labels) > 1 {
		sb.WriteString(styles.Secondary.Render(fmt.Sprintf(" [%s]", strings.Join(labels, "/"))))
	}

	if len(lines) > 1 {
		sb.WriteString("\n")
		sb.WriteString(styles.SecondaryMessage.Render(strings.Join(lines[1:], "\n")))
	}

	return sb.String()
}
