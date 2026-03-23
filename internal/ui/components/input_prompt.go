package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
)

type InputPrompt struct {
	prompt        string
	options       []output.InputOption
	visible       bool
	selectedIndex int
}

func NewInputPrompt() InputPrompt {
	return InputPrompt{}
}

func (p InputPrompt) Show(prompt string, options []output.InputOption) InputPrompt {
	p.prompt = prompt
	p.options = options
	p.visible = true
	p.selectedIndex = 0
	return p
}

func (p InputPrompt) Hide() InputPrompt {
	p.visible = false
	return p
}

func (p InputPrompt) Visible() bool {
	return p.visible
}

func (p InputPrompt) SelectedIndex() int {
	return p.selectedIndex
}

func (p InputPrompt) SetSelectedIndex(idx int) InputPrompt {
	if idx >= 0 && idx < len(p.options) {
		p.selectedIndex = idx
	}
	return p
}

func (p InputPrompt) View() string {
	if !p.visible {
		return ""
	}

	if usesVerticalOptions(p.options) {
		return p.viewVertical()
	}

	lines := strings.Split(p.prompt, "\n")
	firstLine := lines[0]

	var sb strings.Builder
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

func (p InputPrompt) viewVertical() string {
	var sb strings.Builder

	if p.prompt != "" {
		sb.WriteString(styles.Secondary.Render("? "))
		sb.WriteString(styles.Message.Render(strings.TrimPrefix(p.prompt, "? ")))
		sb.WriteString("\n")
	}

	for i, opt := range p.options {
		if i == p.selectedIndex {
			sb.WriteString(styles.NimboMid.Render("● " + opt.Label))
		} else {
			sb.WriteString(styles.Secondary.Render("○ " + opt.Label))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func usesVerticalOptions(options []output.InputOption) bool {
	if len(options) < 2 {
		return false
	}
	for _, opt := range options {
		if strings.Contains(opt.Label, "[") && strings.Contains(opt.Label, "]") {
			return true
		}
	}
	return false
}
