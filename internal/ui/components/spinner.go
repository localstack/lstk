package components

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/ui/styles"
)

type Spinner struct {
	spinner spinner.Model
	message string
	visible bool
}

func NewSpinner() Spinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.Secondary
	return Spinner{
		spinner: s,
	}
}

func (s Spinner) Show(message string) Spinner {
	s.message = message
	s.visible = true
	return s
}

func (s Spinner) Hide() Spinner {
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
	s.spinner, cmd = s.spinner.Update(msg)
	return s, cmd
}

func (s Spinner) Tick() tea.Cmd {
	return s.spinner.Tick
}

func (s Spinner) View() string {
	if !s.visible {
		return ""
	}
	return s.spinner.View() + " " + styles.Secondary.Render(s.message)
}
