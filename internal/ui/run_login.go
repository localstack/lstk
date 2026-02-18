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

func RunLogin(parentCtx context.Context, version string, platformClient api.PlatformAPI) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp(version, cancel)
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		tokenStorage, err := auth.NewTokenStorage()
		if err != nil {
			runErrCh <- err
			p.Send(runErrMsg{err: err})
			return
		}
		a := auth.New(output.NewTUISink(programSender{p: p}), platformClient, tokenStorage, true)

		_, err = a.GetToken(ctx)
		runErrCh <- err
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
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
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	return nil
}
