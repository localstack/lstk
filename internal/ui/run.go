package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/localstack/lstk/internal/update"
	"golang.org/x/term"
)

type programSender struct {
	p *tea.Program
}

func (s programSender) Send(msg any) {
	if s.p == nil {
		return
	}
	s.p.Send(msg)
}

// RunOptions groups the parameters for Run. Bundling them keeps the call
// site readable as the UI entry point grows new concerns.
type RunOptions struct {
	Runtime       runtime.Runtime
	Version       string
	StartOptions  container.StartOptions
	NotifyOptions update.NotifyOptions
	ConfigPath    string
	EmulatorLabel string
	LabelCh       <-chan string
}

func Run(parentCtx context.Context, runOpts RunOptions) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	var appOpts []AppOption
	if runOpts.EmulatorLabel == "" {
		appOpts = append(appOpts, withHeaderLoading())
	}
	app := NewApp(runOpts.Version, runOpts.EmulatorLabel, runOpts.ConfigPath, cancel, appOpts...)
	p := tea.NewProgram(app)
	runErrCh := make(chan error, 1)

	if runOpts.LabelCh != nil {
		go func() {
			select {
			case label, ok := <-runOpts.LabelCh:
				if ok && label != "" {
					p.Send(headerLabelMsg{label: label})
				}
			case <-ctx.Done():
			}
		}()
	}

	go func() {
		var err error
		defer func() { runErrCh <- err }()
		sink := output.NewTUISink(programSender{p: p})
		if update.NotifyUpdate(ctx, sink, runOpts.NotifyOptions) {
			p.Send(runDoneMsg{})
			return
		}
		err = container.Start(ctx, runOpts.Runtime, sink, runOpts.StartOptions, true)
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
		return output.NewSilentError(app.Err())
	}

	runErr := <-runErrCh
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		return runErr
	}

	return nil
}

func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

// runWithTUI runs a TUI command with the given app configuration.
// The runFn is executed in a goroutine and should send runDoneMsg or runErrMsg to the program.
// If the app ends with an error, it's wrapped in SilentError to suppress duplicate output.
func runWithTUI(parentCtx context.Context, appOpts AppOption, runFn func(ctx context.Context, sink output.Sink) error) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp("", "", "", cancel, appOpts)
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		sink := output.NewTUISink(programSender{p: p})
		err := runFn(ctx, sink)
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
