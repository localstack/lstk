package ui

import (
	"context"
	"errors"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/auth"
	"github.com/localstack/lstk/internal/config"
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
	Runtime                runtime.Runtime
	Version                string
	StartOptions           container.StartOptions
	NotifyOptions          update.NotifyOptions
	ConfigPath             string
	EmulatorLabel          string
	NeedsEmulatorSelection bool
}

func Run(parentCtx context.Context, runOpts RunOptions) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	var appOpts []AppOption
	if runOpts.StartOptions.AuthToken == "" {
		appOpts = append(appOpts, withHeaderAfterAuth())
	} else if runOpts.EmulatorLabel == "" {
		appOpts = append(appOpts, withHeaderLoading())
	}
	app := NewApp(runOpts.Version, runOpts.EmulatorLabel, runOpts.ConfigPath, cancel, appOpts...)
	p := tea.NewProgram(app)
	runErrCh := make(chan error, 1)

	labelCh := make(chan string, 1)
	go func() {
		select {
		case label := <-labelCh:
			p.Send(headerLabelMsg{label: label})
		case <-ctx.Done():
		}
	}()

	go func() {
		var err error
		defer func() { runErrCh <- err }()
		sink := output.NewTUISink(programSender{p: p})
		if update.NotifyUpdate(ctx, sink, runOpts.NotifyOptions) {
			p.Send(headerLabelMsg{})
			p.Send(runDoneMsg{})
			return
		}
		if healthErr := runOpts.Runtime.IsHealthy(ctx); healthErr != nil {
			if errors.Is(healthErr, context.Canceled) {
				return
			}
			runOpts.Runtime.EmitUnhealthyError(sink, healthErr)
			p.Send(runErrMsg{err: output.NewSilentError(healthErr)})
			return
		}
		// Resolve the auth token before any emulator-selection prompt so the user
		// logs in first and only configures an emulator once they're authenticated.
		// container.Start still calls GetToken as a safety net for non-interactive
		// callers; once the token is in opts.AuthToken (or the keyring), it returns
		// immediately.
		if authErr := resolveAuthToken(ctx, sink, &runOpts); authErr != nil {
			if errors.Is(authErr, context.Canceled) {
				return
			}
			err = authErr
			p.Send(runErrMsg{err: authErr})
			return
		}
		if runOpts.NeedsEmulatorSelection {
			newContainers, selErr := container.SelectEmulator(ctx, sink, runOpts.ConfigPath)
			if selErr != nil {
				if errors.Is(selErr, context.Canceled) {
					return
				}
				p.Send(runErrMsg{err: selErr})
				return
			}
			runOpts.StartOptions.Containers = newContainers
		}
		var resolvedVersion string
		resolvedVersion, err = container.Start(ctx, runOpts.Runtime, sink, runOpts.StartOptions, true)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			p.Send(runErrMsg{err: err})
			return
		}
		// Empty resolvedVersion means the container was already running and Start
		// returned early — use the cached label rather than re-resolving.
		if resolvedVersion == "" {
			go func() { labelCh <- config.CachedPlanLabel() }()
		} else {
			go container.ResolveAndCacheLabel(ctx, runOpts.StartOptions, resolvedVersion, labelCh)
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

// resolveAuthToken ensures the user is authenticated before the start flow
// continues. On success, the resolved token is written to opts.StartOptions.AuthToken
// so container.Start short-circuits its own auth call.
func resolveAuthToken(ctx context.Context, sink output.Sink, opts *RunOptions) error {
	tokenStorage, err := auth.NewTokenStorage(opts.StartOptions.ForceFileKeyring, opts.StartOptions.Logger)
	if err != nil {
		return err
	}
	a := auth.New(sink, opts.StartOptions.PlatformClient, tokenStorage, opts.StartOptions.AuthToken, opts.StartOptions.WebAppURL, true, "")
	token, err := a.GetToken(ctx)
	if err != nil {
		return err
	}
	opts.StartOptions.AuthToken = token
	return nil
}

func RunMessage(parentCtx context.Context, event output.MessageEvent) error {
	return runWithTUI(parentCtx, withoutHeader(), func(ctx context.Context, sink output.Sink) error {
		sink.Emit(event)
		return nil
	})
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
