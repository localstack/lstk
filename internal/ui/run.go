package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/endpoint"
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

type UpdateNotifyOptions struct {
	GitHubToken    string
	UpdatePrompt   bool
	PersistDisable func() error
}

func Run(parentCtx context.Context, rt runtime.Runtime, version string, opts container.StartOptions, notifyOpts UpdateNotifyOptions) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// FIXME: This assumes a single emulator; revisit for proper multi-emulator support
	emulatorName := "LocalStack Emulator"
	host := endpoint.Hostname
	if len(opts.Containers) > 0 {
		emulatorName = opts.Containers[0].DisplayName()
		if opts.Containers[0].Port != "" {
			host, _ = endpoint.ResolveHost(opts.Containers[0].Port, opts.LocalStackHost)
		}
	}

	app := NewApp(version, emulatorName, host, cancel)
	p := tea.NewProgram(app)
	runErrCh := make(chan error, 1)

	go func() {
		var err error
		defer func() { runErrCh <- err }()
		sink := output.NewTUISink(programSender{p: p})
		if update.NotifyUpdate(ctx, sink, notifyOpts.GitHubToken, notifyOpts.UpdatePrompt, notifyOpts.PersistDisable) {
			p.Send(runDoneMsg{})
			return
		}
		err = container.Start(ctx, rt, sink, opts, true)
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
