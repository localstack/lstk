package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

func RunStop(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, opts container.StopOptions) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp("", "", "", cancel, withoutHeader())
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		err := container.Stop(ctx, rt, output.NewTUISink(programSender{p: p}), containers, opts)
		runErrCh <- err
		if err != nil && !errors.Is(err, context.Canceled) {
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
		return output.NewSilentError(app.Err())
	}

	runErr := <-runErrCh
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	return nil
}
