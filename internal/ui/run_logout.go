package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/ui/components"
	"github.com/localstack/lstk/internal/ui/styles"
)

const minSpinnerDuration = 400 * time.Millisecond

type logoutDoneMsg struct {
	result auth.LogoutResult
}

type logoutErrMsg struct {
	err error
}

type LogoutApp struct {
	header  components.Header
	spinner components.Spinner
	result  *logoutResultDisplay
	err     error
}

type logoutResultDisplay struct {
	success bool
	message string
}

func NewLogoutApp(version string) LogoutApp {
	return LogoutApp{
		header:  components.NewHeader(version),
		spinner: components.NewSpinner().Show("Logging out"),
	}
}

func (a LogoutApp) Init() tea.Cmd {
	return a.spinner.Tick()
}

func (a LogoutApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return a, tea.Quit
		}

	case logoutDoneMsg:
		a.spinner = a.spinner.Hide()
		if msg.result.TokenDeleted {
			a.result = &logoutResultDisplay{success: true, message: "Logged out successfully."}
		} else {
			a.result = &logoutResultDisplay{success: false, message: "Not currently logged in."}
		}
		return a, tea.Quit

	case logoutErrMsg:
		a.spinner = a.spinner.Hide()
		a.err = msg.err
		return a, tea.Quit

	default:
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd
	}

	return a, nil
}

func (a LogoutApp) View() string {
	var s string
	s += a.header.View()
	s += "\n"

	if a.spinner.Visible() {
		s += a.spinner.View() + "\n"
	}

	if a.result != nil {
		s += renderLogoutResult(a.result) + "\n"
	}

	return s
}

func renderLogoutResult(r *logoutResultDisplay) string {
	prefix := styles.Secondary.Render("> ")
	if r.success {
		return prefix + styles.Success.Render("Success:") + " " + styles.Message.Render(r.message)
	}
	return prefix + styles.Note.Render("Note:") + " " + styles.Message.Render(r.message)
}

func (a LogoutApp) Err() error {
	return a.err
}

func RunLogout(ctx context.Context, version string, a *auth.Auth) error {
	app := NewLogoutApp(version)
	p := tea.NewProgram(app)

	go func() {
		start := time.Now()
		result, err := a.Logout()

		// Ensure spinner is visible for minimum duration
		elapsed := time.Since(start)
		if elapsed < minSpinnerDuration {
			time.Sleep(minSpinnerDuration - elapsed)
		}

		if err != nil {
			p.Send(logoutErrMsg{err: err})
			return
		}
		p.Send(logoutDoneMsg{result: result})
	}()

	model, err := p.Run()
	if err != nil {
		return err
	}

	if logoutApp, ok := model.(LogoutApp); ok && logoutApp.Err() != nil {
		return logoutApp.Err()
	}

	return nil
}
