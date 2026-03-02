package components

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/ui/styles"
)

type Spinner struct {
	model   spinner.Model
	text    string
	visible bool
}

func NewSpinner() Spinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.SpinnerStyle
	return Spinner{model: s}
}

func (s Spinner) Start(text string) Spinner {
	s.text = text
	s.visible = true
	return s
}

func (s Spinner) Stop() Spinner {
	s.visible = false
	return s
}

func (s Spinner) Visible() bool {
	return s.visible
}

func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	if !s.visible {
		return s, nil
	}
	var cmd tea.Cmd
	s.model, cmd = s.model.Update(msg)
	return s, cmd
}

func (s Spinner) View() string {
	if !s.visible {
		return ""
	}
	return s.model.View() + " " + styles.Secondary.Render(s.text)
}

func (s Spinner) Tick() tea.Cmd {
	return s.model.Tick
}
