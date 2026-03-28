package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/components"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
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
	message   *output.MessageEvent
}

type App struct {
	header        components.Header
	inputPrompt   components.InputPrompt
	spinner       components.Spinner
	pullProgress  components.PullProgress
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

func withoutHeader() AppOption {
	return func(a *App) { a.hideHeader = true }
}

func NewApp(version, emulatorName, endpoint string, cancel func(), opts ...AppOption) App {
	app := App{
		header:       components.NewHeader(version, emulatorName, endpoint),
		inputPrompt:  components.NewInputPrompt(),
		spinner:      components.NewSpinner(),
		pullProgress: components.NewPullProgress(),
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
			if opt := resolveOption(a.pendingInput.Options, msg); opt != nil {
				responseCmd := sendInputResponseCmd(a.pendingInput.ResponseCh, output.InputResponse{SelectedKey: opt.Key})
				a.pendingInput = nil
				a.inputPrompt = components.NewInputPrompt()
				a.spinner = a.spinner.SetText("")
				return a, responseCmd
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
		msgCopy := msg
		a.addLine(styledLine{text: components.RenderMessage(msg), message: &msgCopy})
		return a, nil
	case output.AuthEvent:
		if msg.Preamble != "" {
			a.lines = appendLine(a.lines, styledLine{text: "> " + msg.Preamble, secondary: true})
		}
		if msg.URL != "" {
			a.lines = appendLine(a.lines, styledLine{text: "Opening browser to login..."})
			a.lines = appendLine(a.lines, styledLine{text: "Browser didn't open? Visit " + msg.URL, secondary: true})
		}
		if msg.Code != "" {
			a.lines = appendLine(a.lines, styledLine{text: ""})
			a.lines = appendLine(a.lines, styledLine{text: styles.SecondaryMessage.Render("One-time code: ") + styles.NimboMid.Render(msg.Code)})
			a.lines = appendLine(a.lines, styledLine{text: ""})
		}
		return a, nil
	case output.LogLineEvent:
		prefix := styles.Secondary.Render(msg.Source + " | ")
		a.addLine(styledLine{text: prefix + renderLogLine(msg.Line, msg.Level)})
		return a, nil
	case output.ContainerStatusEvent:
		if msg.Phase == "pulling" {
			a.pullProgress = a.pullProgress.Show(msg.Container)
			return a, nil
		}
		if a.pullProgress.Visible() {
			a.pullProgress = a.pullProgress.Hide()
		}
		if line, ok := output.FormatEventLine(msg); ok {
			if msg.Phase == "ready" {
				line = strings.Replace(line, output.SuccessMarker(), styles.Success.Render(output.SuccessMarker()), 1)
			}
			a.lines = appendLine(a.lines, styledLine{text: line})
		}
		return a, nil
	case output.ProgressEvent:
		if a.pullProgress.Visible() {
			var cmd tea.Cmd
			a.pullProgress, cmd = a.pullProgress.SetProgress(msg)
			return a, cmd
		}
		return a, nil
	case progress.FrameMsg:
		var cmd tea.Cmd
		a.pullProgress, cmd = a.pullProgress.Update(msg)
		return a, cmd
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
		if !output.IsSilent(msg.err) {
			a.errorDisplay = a.errorDisplay.Show(output.ErrorEvent{Title: msg.err.Error()})
		}
		return a, tea.Quit
	case output.TableEvent:
		if line, ok := output.FormatEventLine(msg); ok {
			parts := strings.Split(line, "\n")
			if len(parts) > 0 {
				a.addLine(styledLine{text: parts[0], secondary: true})
			}
			for _, part := range parts[1:] {
				a.addLine(styledLine{text: part})
			}
		}
	case output.ResourceSummaryEvent:
		text := fmt.Sprintf("~ %d resources · %d services", msg.Resources, msg.Services)
		style := styles.Message
		if msg.Resources > 0 && msg.Services > 0 {
			style = styles.Highlight
		}
		blank := styledLine{text: ""}
		a.addLine(blank)
		a.addLine(styledLine{text: style.Render(text)})
		a.addLine(blank)
		return a, nil
	case output.InstanceInfoEvent:
		if line, ok := output.FormatEventLine(msg); ok {
			line = strings.Replace(line, output.SuccessMarker(), styles.Success.Render(output.SuccessMarker()), 1)
			for _, part := range strings.Split(line, "\n") {
				a.addLine(styledLine{text: part})
			}
		}
		return a, nil
	default:
		if line, ok := output.FormatEventLine(msg); ok {
			for _, part := range strings.Split(line, "\n") {
				a.addLine(styledLine{text: part})
			}
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

func (a *App) addLine(line styledLine) {
	if a.spinner.PendingStop() {
		a.bufferedLines = appendLine(a.bufferedLines, line)
	} else {
		a.lines = appendLine(a.lines, line)
	}
}

func (a *App) flushBufferedLines() {
	for _, line := range a.bufferedLines {
		a.lines = appendLine(a.lines, line)
	}
	a.bufferedLines = nil
}

// resolveOption finds the best matching option for a key event, in priority order:
//  1. "any" — matches any keypress
//  2. "enter" — matches the Enter key explicitly
//  3. uppercase label — matches Enter as the conventional default
//  4. case-insensitive key match — matches any other key
func resolveOption(options []output.InputOption, msg tea.KeyMsg) *output.InputOption {
	var uppercaseDefault *output.InputOption
	for i, opt := range options {
		switch {
		case opt.Key == "any":
			return &options[i]
		case msg.Type == tea.KeyEnter && opt.Key == "enter":
			return &options[i]
		case msg.Type == tea.KeyEnter && uppercaseDefault == nil &&
			opt.Label != "" && hasLetters(opt.Label) && opt.Label == strings.ToUpper(opt.Label):
			uppercaseDefault = &options[i]
		case msg.Type != tea.KeyEnter && strings.EqualFold(msg.String(), opt.Key):
			return &options[i]
		}
	}
	return uppercaseDefault
}

func hasLetters(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
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

	for _, line := range a.lines {
		if line.message != nil {
			sb.WriteString(components.RenderWrappedMessage(*line.message, a.width))
			sb.WriteString("\n")
			continue
		}
		if line.highlight {
			if isURL(line.text) {
				wrapped := strings.Split(wrap.HardWrap(line.text, a.width), "\n")
				var styledParts []string
				for _, part := range wrapped {
					styledParts = append(styledParts, styles.Link.Render(part))
				}
				sb.WriteString(hyperlink(line.text, strings.Join(styledParts, "\n")))
			} else {
				sb.WriteString(styles.Highlight.Render(wrap.HardWrap(line.text, a.width)))
			}
			sb.WriteString("\n\n")
			continue
		} else if line.secondary {
			if strings.HasPrefix(line.text, ">") {
				sb.WriteString(styles.SecondaryMessage.Render(wrap.HardWrap(line.text, a.width)))
				sb.WriteString("\n\n")
				continue
			}
			text := wrap.HardWrap(line.text, a.width)
			sb.WriteString(styles.SecondaryMessage.Render(text))
		} else {
			text := wrap.HardWrap(line.text, a.width)
			sb.WriteString(text)
		}
		sb.WriteString("\n")
	}

	if spinnerView := a.spinner.View(); spinnerView != "" {
		sb.WriteString(spinnerView)
		sb.WriteString("\n")
		if pullView := a.pullProgress.View(); pullView != "" {
			sb.WriteString(pullView)
			sb.WriteString("\n")
		}
	} else if promptView := a.inputPrompt.View(); promptView != "" {
		sb.WriteString(promptView)
		sb.WriteString("\n")
	}

	if errorView := a.errorDisplay.View(a.width); errorView != "" {
		sb.WriteString(errorView)
	}

	return sb.String()
}

func (a App) Err() error {
	return a.err
}
