package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/endpoint"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// RunStatusExternal is RunStatus for an externally-managed endpoint
// (--endpoint-url and friends). Targeting an emulator lstk didn't start
// changes which facts are available, not how they're rendered, so the
// interactive path stays the TUI here too.
func RunStatusExternal(parentCtx context.Context, target *endpoint.Target, clients map[config.EmulatorType]emulator.Client) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		return container.StatusExternal(ctx, target, clients, sink)
	})
}

func RunStatus(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, localStackHost string, clients map[config.EmulatorType]emulator.Client) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp("", "", "", cancel, withoutHeader())
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		err := container.Status(ctx, rt, containers, localStackHost, clients, output.NewTUISink(programSender{p: p}))
		if err != nil && !errors.Is(err, context.Canceled) {
			p.Send(runErrMsg{err: err})
		} else {
			p.Send(runDoneMsg{})
		}
		runErrCh <- err
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
