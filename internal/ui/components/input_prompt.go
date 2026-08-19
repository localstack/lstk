package components

import (
	"strings"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
)

type InputPrompt struct {
	prompt        string
	options       []output.InputOption
	visible       bool
	selectedIndex int
	vertical      bool
}

func NewInputPrompt() InputPrompt {
	return InputPrompt{}
}

func (p InputPrompt) Show(prompt string, options []output.InputOption, vertical bool) InputPrompt {
	p.prompt = prompt
	p.options = options
	p.visible = true
	p.selectedIndex = 0
	p.vertical = vertical
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

// marker prefixes the question; continuation lines are indented past it.
const marker = "? "

// View renders the prompt, wrapped to width so Bubble Tea's renderer cannot
// truncate away the part that says which key to press (DEVX-1045). A width of 0
// (before the first WindowSizeMsg) or a width too narrow for the marker renders
// unwrapped.
func (p InputPrompt) View(width int) string {
	if !p.visible {
		return ""
	}

	if p.vertical {
		return p.viewVertical(width)
	}

	lines := strings.Split(p.prompt, "\n")
	question, suffix := lines[0], output.FormatPromptLabels(p.options)

	var sb strings.Builder
	sb.WriteString(styles.Secondary.Render(marker))
	sb.WriteString(renderWrappedPrompt(question, suffix, width))

	if len(lines) > 1 {
		sb.WriteString("\n")
		sb.WriteString(styles.SecondaryMessage.Render(strings.Join(lines[1:], "\n")))
	}

	return sb.String()
}

// renderWrappedPrompt wraps the question to the width left of the marker and
// appends the key hints, keeping them dimmed and on the same line when they fit.
// The hints are wrapped as a unit rather than word by word, so they never end up
// split across lines.
func renderWrappedPrompt(question, suffix string, width int) string {
	indent := strings.Repeat(" ", len([]rune(marker)))
	available := width - len([]rune(marker))
	if available <= 0 {
		return styles.Message.Render(question) + styles.Secondary.Render(suffix)
	}

	lines := wrap.SoftWrap(question, available)
	if len(lines) == 0 {
		lines = []string{""}
	}

	rendered := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		rendered = append(rendered, styles.Message.Render(line))
	}

	if suffix != "" {
		last := len(lines) - 1
		if len([]rune(lines[last]))+len([]rune(suffix)) <= available {
			rendered[last] += styles.Secondary.Render(suffix)
		} else {
			rendered = append(rendered, styles.Secondary.Render(strings.TrimPrefix(suffix, " ")))
		}
	}

	return strings.Join(rendered, "\n"+indent)
}

func (p InputPrompt) viewVertical(width int) string {
	var sb strings.Builder

	if p.prompt != "" {
		sb.WriteString(styles.Secondary.Render(marker))
		sb.WriteString(renderWrappedPrompt(p.prompt, "", width))
		sb.WriteString("\n")
	}

	for i, opt := range p.options {
		label := output.OptionLabel(opt)
		if i == p.selectedIndex {
			sb.WriteString(styles.NimboMid.Render("● " + label))
		} else {
			sb.WriteString(styles.Secondary.Render("○ " + label))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

