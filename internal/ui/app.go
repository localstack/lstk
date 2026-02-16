package ui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/components"
	"github.com/localstack/lstk/internal/ui/styles"
)

const maxLines = 200

type runDoneMsg struct{}

type runErrMsg struct {
	err error
}

type App struct {
	header  components.Header
	lines   []string
	cancel  func()
	onEnter func()
	err     error
}

func NewApp(version string, cancel func(), onEnter func()) App {
	return App{
		header:  components.NewHeader(version),
		lines:   make([]string, 0, maxLines),
		cancel:  cancel,
		onEnter: onEnter,
	}
}

func (a App) Init() tea.Cmd {
	return nil
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			if a.cancel != nil {
				a.cancel()
			}
			a.err = context.Canceled
			return a, tea.Quit
		}
		if msg.Type == tea.KeyEnter {
			if a.onEnter != nil {
				a.onEnter()
			}
		}
	case runDoneMsg:
		return a, tea.Quit
	case runErrMsg:
		a.err = msg.err
		return a, tea.Quit
	default:
		if line, ok := output.FormatEventLine(msg); ok {
			a.lines = appendLine(a.lines, line)
		}
	}

	return a, nil
}

func appendLine(lines []string, line string) []string {
	lines = append(lines, line)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func (a App) View() string {
	var sb strings.Builder
	sb.WriteString(a.header.View())
	sb.WriteString("\n")
	for _, line := range a.lines {
		sb.WriteString("  ")
		sb.WriteString(styles.Message.Render(line))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (a App) Err() error {
	return a.err
}
