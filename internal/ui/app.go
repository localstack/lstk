package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

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

type copiedResetMsg struct{}

type clipboardResultMsg struct {
	ok bool
}

type styledLine struct {
	text      string
	highlight bool
	secondary bool
}

type App struct {
	header       components.Header
	inputPrompt  components.InputPrompt
	lines        []styledLine
	width        int
	cancel       func()
	pendingInput *output.UserInputRequestEvent
	copiedFlash  bool
	err          error
}

func NewApp(version string, cancel func()) App {
	return App{
		header:      components.NewHeader(version),
		inputPrompt: components.NewInputPrompt(),
		lines:       make([]styledLine, 0, maxLines),
		cancel:      cancel,
	}
}

func (a App) Init() tea.Cmd {
	return nil
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
			// A single character key press selects the matching option
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
		if msg.String() == "c" {
			if url := a.findURL(); url != "" {
				return a, clipboardCmd(url)
			}
		}
	case tea.WindowSizeMsg:
		a.width = msg.Width
	case output.UserInputRequestEvent:
		a.pendingInput = &msg
		a.inputPrompt = a.inputPrompt.Show(msg.Prompt, msg.Options)
	case copiedResetMsg:
		a.copiedFlash = false
	case clipboardResultMsg:
		a.copiedFlash = msg.ok
		if msg.ok {
			return a, tea.Tick(time.Second, func(time.Time) tea.Msg {
				return copiedResetMsg{}
			})
		}
	case runDoneMsg:
		return a, tea.Quit
	case runErrMsg:
		a.err = msg.err
		return a, tea.Quit
	case output.SecondaryLogEvent:
		a.lines = appendLine(a.lines, styledLine{text: msg.Message, secondary: true})
	case output.HighlightLogEvent:
		a.lines = appendLine(a.lines, styledLine{text: msg.Message, highlight: true})
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

func formatResolvedInput(req output.UserInputRequestEvent, selectedKey string) string {
	firstLine := strings.Split(req.Prompt, "\n")[0]
	labels := make([]string, 0, len(req.Options))
	selected := selectedKey

	for _, opt := range req.Options {
		if opt.Label != "" {
			labels = append(labels, opt.Label)
		}
		if opt.Key == selectedKey && opt.Label != "" {
			selected = opt.Label
		}
	}

	switch len(labels) {
	case 1:
		firstLine = fmt.Sprintf("%s (%s)", firstLine, labels[0])
	default:
		if len(labels) > 1 {
			firstLine = fmt.Sprintf("%s [%s]", firstLine, strings.Join(labels, "/"))
		}
	}

	if selected == "" || len(labels) == 0 || (len(labels) == 1 && selected == labels[0]) {
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

func (a App) findURL() string {
	for i := len(a.lines) - 1; i >= 0; i-- {
		line := a.lines[i]
		if line.highlight && isURL(line.text) {
			return line.text
		}
	}
	return ""
}

func clipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardResultMsg{ok: copyToClipboard(text) == nil}
	}
}

func copyToClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		return runClipboardCandidates(text, [][]string{
			{"pbcopy"},
		})
	case "linux":
		candidates := [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
		if strings.EqualFold(os.Getenv("XDG_SESSION_TYPE"), "wayland") || os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append([][]string{{"wl-copy"}}, candidates...)
		}
		return runClipboardCandidates(text, candidates)
	case "windows":
		return runClipboardCandidates(text, [][]string{
			{"cmd", "/c", "clip"},
		})
	default:
		return fmt.Errorf("clipboard copy not supported on %s", runtime.GOOS)
	}
}

func runClipboardCandidates(text string, candidates [][]string) error {
	var errs []string
	for _, candidate := range candidates {
		cmd := exec.Command(candidate[0], candidate[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Sprintf("%s: %v", strings.Join(candidate, " "), err))
		}
	}
	return fmt.Errorf("copy to clipboard failed: %s", strings.Join(errs, "; "))
}

func nextIsURL(lines []styledLine, i int) bool {
	if i+1 < len(lines) {
		next := lines[i+1]
		return next.highlight && isURL(next.text)
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
	sb.WriteString(a.header.View())
	sb.WriteString("\n")
	contentWidth := a.width - lineIndent
	for i, line := range a.lines {
		sb.WriteString("  ")
		if line.highlight {
			if isURL(line.text) {
				wrapped := strings.Split(hardWrap(line.text, contentWidth), "\n")
				var styledParts []string
				for _, part := range wrapped {
					styledParts = append(styledParts, styles.Link.Render(part))
				}
				sb.WriteString(hyperlink(line.text, strings.Join(styledParts, "\n  ")))
			} else {
				sb.WriteString(styles.Highlight.Render(hardWrap(line.text, contentWidth)))
			}
		} else if line.secondary {
			text := hardWrap(line.text, contentWidth)
			sb.WriteString(styles.SecondaryMessage.Render(text))
		} else {
			text := hardWrap(line.text, contentWidth)
			sb.WriteString(styles.Message.Render(text))
			if nextIsURL(a.lines, i) {
				sb.WriteString(" ")
				if a.copiedFlash {
					sb.WriteString(styles.SecondaryMessage.Render("(copied!)"))
				} else {
					sb.WriteString(styles.SecondaryMessage.Render("(c to copy)"))
				}
			}
		}
		sb.WriteString("\n")
	}
	if promptView := a.inputPrompt.View(); promptView != "" {
		sb.WriteString("  ")
		sb.WriteString(promptView)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (a App) Err() error {
	return a.err
}
