package ui

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/ui/components"
	"github.com/localstack/lstk/internal/ui/styles"
)

const minSpinnerDuration = 400 * time.Millisecond

type logoutSuccessMsg struct{}

type logoutNotLoggedInMsg struct{}

type logoutErrMsg struct {
	err error
}

type logoutState int

const (
	logoutStateLoading logoutState = iota
	logoutStateSuccess
	logoutStateNotLoggedIn
)

type LogoutApp struct {
	header  components.Header
	spinner components.Spinner
	state   logoutState
	err     error
}

func NewLogoutApp(version string) LogoutApp {
	return LogoutApp{
		header:  components.NewHeader(version),
		spinner: components.NewSpinner().Show("Logging out"),
		state:   logoutStateLoading,
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

	case logoutSuccessMsg:
		a.spinner = a.spinner.Hide()
		a.state = logoutStateSuccess
		return a, tea.Quit

	case logoutNotLoggedInMsg:
		a.spinner = a.spinner.Hide()
		a.state = logoutStateNotLoggedIn
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

	switch a.state {
	case logoutStateSuccess:
		s += renderLogoutSuccess() + "\n"
	case logoutStateNotLoggedIn:
		s += renderLogoutNotLoggedIn() + "\n"
	}

	return s
}

func renderLogoutSuccess() string {
	prefix := styles.Secondary.Render("> ")
	return prefix + styles.Success.Render("Success:") + " " + styles.Message.Render("Logged out successfully.")
}

func renderLogoutNotLoggedIn() string {
	prefix := styles.Secondary.Render("> ")
	return prefix + styles.Note.Render("Note:") + " " + styles.Message.Render("Not currently logged in.")
}

func (a LogoutApp) Err() error {
	return a.err
}

func RunLogout(ctx context.Context, version string, a *auth.Auth) error {
	app := NewLogoutApp(version)
	p := tea.NewProgram(app)

	go func() {
		start := time.Now()
		err := a.Logout()

		// Ensure spinner is visible for minimum duration
		elapsed := time.Since(start)
		if elapsed < minSpinnerDuration {
			time.Sleep(minSpinnerDuration - elapsed)
		}

		if errors.Is(err, auth.ErrNotLoggedIn) {
			p.Send(logoutNotLoggedInMsg{})
			return
		}
		if err != nil {
			p.Send(logoutErrMsg{err: err})
			return
		}
		p.Send(logoutSuccessMsg{})
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
