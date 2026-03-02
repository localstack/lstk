package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/components"
)

const maxLines = 200

type runDoneMsg struct{}

type runErrMsg struct {
	err error
}

type App struct {
	header       components.Header
	inputPrompt  components.InputPrompt
	spinner      components.Spinner
	errorDisplay components.ErrorDisplay
	lines        []string
	cancel       func()
	pendingInput *output.UserInputRequestEvent
	err          error
}

func NewApp(version string, cancel func()) App {
	return App{
		header:       components.NewHeader(version),
		inputPrompt:  components.NewInputPrompt(),
		spinner:      components.NewSpinner(),
		errorDisplay: components.NewErrorDisplay(),
		lines:        make([]string, 0, maxLines),
		cancel:       cancel,
	}
}

func (a App) Init() tea.Cmd {
	return a.spinner.Tick()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			var responseCmd tea.Cmd
			if a.pendingInput != nil {
				responseCmd = sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{Cancelled: true})
				a.pendingInput = nil
				a.inputPrompt = a.inputPrompt.Hide()
			}
			if a.cancel != nil {
				a.cancel()
			}
			a.err = context.Canceled
			if responseCmd != nil {
				return a, func() tea.Msg {
					responseCmd()
					return tea.QuitMsg{}
				}
			}
			return a, tea.Quit
		}
		if a.pendingInput != nil {
			if msg.Type == tea.KeyEnter {
				// ENTER selects the first option (default)
				selectedKey := ""
				if len(a.pendingInput.Options) > 0 {
					selectedKey = a.pendingInput.Options[0].Key
				}
				responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: selectedKey})
				a.pendingInput = nil
				a.inputPrompt = a.inputPrompt.Hide()
				return a, responseCmd
			}
			// A single character key press selects the matching option
			for _, opt := range a.pendingInput.Options {
				if msg.String() == opt.Key {
					responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: opt.Key})
					a.pendingInput = nil
					a.inputPrompt = a.inputPrompt.Hide()
					return a, responseCmd
				}
			}
		}
	case output.UserInputRequestEvent:
		a.pendingInput = &msg
		a.inputPrompt = a.inputPrompt.Show(msg.Prompt, msg.Options)
	case spinner.TickMsg:
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd
	case output.SpinnerEvent:
		if msg.Active {
			a.spinner = a.spinner.Start(msg.Text)
			return a, a.spinner.Tick()
		}
		a.spinner = a.spinner.Stop()
		return a, nil
	case output.ErrorEvent:
		a.errorDisplay = a.errorDisplay.Show(msg)
		a.spinner = a.spinner.Stop()
		return a, nil
	case output.MessageEvent:
		line := components.RenderMessage(msg)
		a.lines = appendLine(a.lines, line)
		return a, nil
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

func sendInputResponseCmd(responseCh chan<- output.InputResponse, response output.InputResponse) tea.Cmd {
	if responseCh == nil {
		return nil
	}

	return func() tea.Msg {
		go func() {
			responseCh <- response
		}()
		return nil
	}
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

	if spinnerView := a.spinner.View(); spinnerView != "" {
		sb.WriteString("  ")
		sb.WriteString(spinnerView)
		sb.WriteString("\n")
	}

	for _, line := range a.lines {
		sb.WriteString("  ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	if promptView := a.inputPrompt.View(); promptView != "" {
		sb.WriteString("  ")
		sb.WriteString(promptView)
		sb.WriteString("\n")
	}

	if errorView := a.errorDisplay.View(); errorView != "" {
		sb.WriteString("\n")
		sb.WriteString(errorView)
	}

	return sb.String()
}

func (a App) Err() error {
	return a.err
}
