package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/output"
)

func RunLogout(parentCtx context.Context, platformClient api.PlatformAPI, authToken string, forceFileKeyring bool) error {
	_, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp("", "", "", cancel, withoutHeader())

	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		tokenStorage, err := auth.NewTokenStorage(forceFileKeyring)
		if err != nil {
			runErrCh <- err
			p.Send(runErrMsg{err: err})
			return
		}

		a := auth.New(output.NewTUISink(programSender{p: p}), platformClient, tokenStorage, authToken, "", false)
		err = a.Logout()

		runErrCh <- err
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, auth.ErrNotLoggedIn) {
			p.Send(runErrMsg{err: err})
			return
		}
		p.Send(runDoneMsg{})
	}()

	model, err := p.Run()
	if err != nil {
		return err
	}

	if app, ok := model.(App); ok && app.Err() != nil {
		return app.Err()
	}

	runErr := <-runErrCh
	if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, auth.ErrNotLoggedIn) {
		return runErr
	}

	return nil
}
