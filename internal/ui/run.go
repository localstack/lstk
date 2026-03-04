package ui

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/api"
	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/container"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
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

func Run(parentCtx context.Context, rt runtime.Runtime, version string, platformClient api.PlatformAPI, authToken string, forceFileBackend bool, webAppURL string) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// FIXME: This assumes a single emulator; revisit for proper multi-emulator support
	emulatorName := "LocalStack Emulator"
	endpoint := "localhost.localstack.cloud"
	if cfg, err := config.Get(); err == nil && len(cfg.Containers) > 0 {
		emulatorName = cfg.Containers[0].DisplayName()
		if cfg.Containers[0].Port != "" {
			endpoint = fmt.Sprintf("localhost.localstack.cloud:%s", cfg.Containers[0].Port)
		}
	}

	app := NewApp(version, emulatorName, endpoint, cancel)
	p := tea.NewProgram(app)
	runErrCh := make(chan error, 1)

	go func() {
		var err error
		defer func() { runErrCh <- err }()
		err = container.Start(ctx, rt, output.NewTUISink(programSender{p: p}), platformClient, authToken, forceFileBackend, webAppURL, true)
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

func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}
