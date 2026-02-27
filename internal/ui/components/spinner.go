package components

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/ui/styles"
)

type SpinnerMinDurationElapsedMsg struct{}

type Spinner struct {
	model       spinner.Model
	text        string
	visible     bool
	startedAt   time.Time
	minDuration time.Duration
	pendingStop bool
}

func NewSpinner() Spinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = styles.SpinnerStyle
	return Spinner{model: s}
}

func (s Spinner) Start(text string, minDuration time.Duration) Spinner {
	s.text = text
	s.visible = true
	s.startedAt = time.Now()
	s.pendingStop = false
	s.minDuration = minDuration
	return s
}

func (s Spinner) Stop() (Spinner, tea.Cmd) {
	elapsed := time.Since(s.startedAt)
	remaining := s.minDuration - elapsed

	if remaining <= 0 {
		s.visible = false
		s.pendingStop = false
		return s, nil
	}

	s.pendingStop = true
	return s, tea.Tick(remaining, func(t time.Time) tea.Msg {
		return SpinnerMinDurationElapsedMsg{}
	})
}

func (s Spinner) PendingStop() bool {
	return s.pendingStop
}

func (s Spinner) HandleMinDurationElapsed() Spinner {
	if s.pendingStop {
		s.visible = false
		s.pendingStop = false
	}
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
