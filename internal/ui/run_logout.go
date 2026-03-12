package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func RunLogout(parentCtx context.Context, rt runtime.Runtime, platformClient api.PlatformAPI, authToken string, forceFileKeyring bool, containers []config.ContainerConfig) error {
	ctx, cancel := context.WithCancel(parentCtx)
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

		sink := output.NewTUISink(programSender{p: p})
		a := auth.New(sink, platformClient, tokenStorage, authToken, "", false)
		err = a.Logout()
		if err == nil && rt != nil {
			if running, runningErr := container.AnyRunning(ctx, rt, containers); runningErr == nil && running {
				output.EmitNote(sink, "LocalStack is still running in the background")
			}
		}

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
