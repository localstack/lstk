package components

import (
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
	text := p.prompt
	if len(p.options) > 0 {
		text += " (" + p.options[0].Label + ")"
	}
	return styles.Message.Render(text)
}
