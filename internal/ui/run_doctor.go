package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/doctor"
	"github.com/localstack/lstk/internal/emulator"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

const doctorUIRunTimeout = 45 * time.Second

func RunDoctor(parentCtx context.Context, rt runtime.Runtime, emulatorClient emulator.Client, opts doctor.Options) error {
	ctx, cancel := context.WithTimeout(parentCtx, doctorUIRunTimeout)
	defer cancel()

	app := NewApp("", "", "", cancel, withoutHeader())
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)
	timeoutErr := fmt.Errorf("diagnostic checks timed out after %s", doctorUIRunTimeout)

	go func() {
		err := doctor.Run(ctx, rt, emulatorClient, output.NewTUISink(programSender{p: p}), opts)
		if err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			p.Send(runErrMsg{err: err})
		} else if ctx.Err() == nil {
			p.Send(runDoneMsg{})
		}
		runErrCh <- err
	}()

	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			p.Send(runErrMsg{err: timeoutErr})
		}
	}()

	model, err := p.Run()
	if err != nil {
		return err
	}

	if app, ok := model.(App); ok && app.Err() != nil {
		return output.NewSilentError(app.Err())
	}

	select {
	case runErr := <-runErrCh:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.NewSilentError(timeoutErr)
		}
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return runErr
		}
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return output.NewSilentError(timeoutErr)
		}
		return ctx.Err()
	}

	return nil
}
