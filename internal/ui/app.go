package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
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

type styledLine struct {
	text      string
	highlight bool
	secondary bool
}

type App struct {
	header        components.Header
	inputPrompt   components.InputPrompt
	spinner       components.Spinner
	errorDisplay  components.ErrorDisplay
	lines         []styledLine
	bufferedLines []styledLine // lines waiting for spinner to finish
	width         int
	cancel        func()
	pendingInput  *output.UserInputRequestEvent
	err           error
	quitting      bool
	hideHeader    bool
}

type AppOption func(*App)

var WithoutHeader AppOption = func(a *App) {
	a.hideHeader = true
}

func NewApp(version, emulatorName, endpoint string, cancel func(), opts ...AppOption) App {
	app := App{
		header:       components.NewHeader(version, emulatorName, endpoint),
		inputPrompt:  components.NewInputPrompt(),
		spinner:      components.NewSpinner(),
		errorDisplay: components.NewErrorDisplay(),
		lines:        make([]styledLine, 0, maxLines),
		cancel:       cancel,
	}
	for _, opt := range opts {
		opt(&app)
	}
	return app
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
			// "any" option: any keypress resolves the prompt
			for _, opt := range a.pendingInput.Options {
				if opt.Key == "any" {
					a.lines = appendLine(a.lines, styledLine{text: formatResolvedInput(*a.pendingInput, "any")})
					responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: "any"})
					a.pendingInput = nil
					a.inputPrompt = a.inputPrompt.Hide()
					return a, responseCmd
				}
			}
			if msg.Type == tea.KeyEnter {
				for _, opt := range a.pendingInput.Options {
					if opt.Key == "enter" {
						a.lines = appendLine(a.lines, styledLine{text: formatResolvedInput(*a.pendingInput, "enter")})
						responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: "enter"})
						a.pendingInput = nil
						a.inputPrompt = a.inputPrompt.Hide()
						return a, responseCmd
					}
				}
				return a, nil
			}
			for _, opt := range a.pendingInput.Options {
				if msg.String() == opt.Key {
					a.lines = appendLine(a.lines, styledLine{text: formatResolvedInput(*a.pendingInput, opt.Key)})
					responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: opt.Key})
					a.pendingInput = nil
					a.inputPrompt = a.inputPrompt.Hide()
					return a, responseCmd
				}
			}
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
	case output.UserInputRequestEvent:
		a.pendingInput = &msg
		if a.spinner.Visible() {
			a.spinner = a.spinner.SetText(output.FormatPrompt(msg.Prompt, msg.Options))
		} else {
			a.inputPrompt = a.inputPrompt.Show(msg.Prompt, msg.Options)
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd
	case output.SpinnerEvent:
		if msg.Active {
			a.spinner = a.spinner.Start(msg.Text, msg.MinDuration)
			return a, a.spinner.Tick()
		}
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Stop()
		if !a.spinner.PendingStop() {
			a.flushBufferedLines()
			if a.quitting {
				return a, tea.Quit
			}
		}
		return a, cmd
	case components.SpinnerMinDurationElapsedMsg:
		a.spinner = a.spinner.HandleMinDurationElapsed()
		a.flushBufferedLines()
		if a.quitting {
			return a, tea.Quit
		}
		return a, nil
	case output.ErrorEvent:
		a.errorDisplay = a.errorDisplay.Show(msg)
		a.spinner, _ = a.spinner.Stop()
		return a, nil
	case output.MessageEvent:
		line := styledLine{text: components.RenderMessage(msg)}
		if a.spinner.PendingStop() {
			a.bufferedLines = append(a.bufferedLines, line)
		} else {
			a.lines = appendLine(a.lines, line)
		}
		return a, nil
	case output.AuthEvent:
		if msg.Preamble != "" {
			a.lines = appendLine(a.lines, styledLine{text: "> " + msg.Preamble, secondary: true})
		}
		if msg.Code != "" {
			a.lines = appendLine(a.lines, styledLine{text: "Your one-time code:"})
			a.lines = appendLine(a.lines, styledLine{text: msg.Code, highlight: true})
		}
		if msg.URL != "" {
			a.lines = appendLine(a.lines, styledLine{text: "Opening browser to login..."})
			a.lines = appendLine(a.lines, styledLine{text: msg.URL, secondary: true})
		}
		return a, nil
	case runDoneMsg:
		if a.spinner.PendingStop() {
			a.quitting = true
			return a, nil
		}
		return a, tea.Quit
	case runErrMsg:
		a.err = msg.err
		a.spinner, _ = a.spinner.Stop()
		a.flushBufferedLines()
		return a, tea.Quit
	default:
		if line, ok := output.FormatEventLine(msg); ok {
			a.lines = appendLine(a.lines, styledLine{text: line})
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

func appendLine(lines []styledLine, line styledLine) []styledLine {
	lines = append(lines, line)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func (a *App) flushBufferedLines() {
	for _, line := range a.bufferedLines {
		a.lines = appendLine(a.lines, line)
	}
	a.bufferedLines = nil
}

func formatResolvedInput(req output.UserInputRequestEvent, selectedKey string) string {
	formatted := output.FormatPrompt(req.Prompt, req.Options)
	firstLine := strings.Split(formatted, "\n")[0]

	selected := selectedKey
	hasLabels := false
	for _, opt := range req.Options {
		if opt.Label != "" {
			hasLabels = true
		}
		if opt.Key == selectedKey && opt.Label != "" {
			selected = opt.Label
		}
	}

	if selected == "" || !hasLabels {
		return firstLine
	}
	return fmt.Sprintf("%s %s", firstLine, selected)
}

const lineIndent = 2

func hardWrap(s string, maxWidth int) string {
	rs := []rune(s)
	if maxWidth <= 0 || len(rs) <= maxWidth {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(rs); i += maxWidth {
		if i > 0 {
			sb.WriteByte('\n')
		}
		end := i + maxWidth
		if end > len(rs) {
			end = len(rs)
		}
		sb.WriteString(string(rs[i:end]))
	}
	return sb.String()
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func hyperlink(url, displayText string) string {
	return fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, displayText)
}

func (a App) View() string {
	var sb strings.Builder
	if !a.hideHeader {
		sb.WriteString(a.header.View())
		sb.WriteString("\n")
	}

	indent := strings.Repeat(" ", lineIndent)
	contentWidth := a.width - lineIndent
	for _, line := range a.lines {
		if line.highlight {
			if isURL(line.text) {
				wrapped := strings.Split(hardWrap(line.text, contentWidth), "\n")
				var styledParts []string
				for _, part := range wrapped {
					styledParts = append(styledParts, styles.Link.Render(part))
				}
				sb.WriteString(indent)
				sb.WriteString(hyperlink(line.text, strings.Join(styledParts, "\n"+indent)))
			} else {
				sb.WriteString(indent)
				sb.WriteString(styles.Highlight.Render(hardWrap(line.text, contentWidth)))
			}
			sb.WriteString("\n\n")
			continue
		} else if line.secondary {
			if strings.HasPrefix(line.text, ">") {
				sb.WriteString(styles.SecondaryMessage.Render(hardWrap(line.text, contentWidth)))
				sb.WriteString("\n\n")
				continue
			}
			sb.WriteString(indent)
			text := hardWrap(line.text, contentWidth)
			sb.WriteString(styles.SecondaryMessage.Render(text))
		} else {
			sb.WriteString(indent)
			text := hardWrap(line.text, contentWidth)
			sb.WriteString(text)
		}
		sb.WriteString("\n")
	}

	if spinnerView := a.spinner.View(); spinnerView != "" {
		sb.WriteString(spinnerView)
		sb.WriteString("\n")
	} else if promptView := a.inputPrompt.View(); promptView != "" {
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
