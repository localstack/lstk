package ui

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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
			if label != "" {
				p.Send(headerLabelMsg{label: label})
			}
		case <-ctx.Done():
		}
	}()

	go func() {
		var err error
		defer func() { runErrCh <- err }()
		sink := output.NewTUISink(programSender{p: p})
		// Start label resolution immediately when no emulator selection is needed, so
		// headerLabelMsg always arrives even if NotifyUpdate returns early (update case).
		// When emulator selection is needed, resolution starts after the user picks.
		if !runOpts.NeedsEmulatorSelection {
			go resolveAndCacheLabel(ctx, runOpts.StartOptions, labelCh)
		}
		if update.NotifyUpdate(ctx, sink, runOpts.NotifyOptions) {
			p.Send(runDoneMsg{})
			return
		}
		if runOpts.NeedsEmulatorSelection {
			newContainers, selErr := selectEmulatorInTUI(ctx, sink, runOpts.ConfigPath)
			if selErr != nil {
				if errors.Is(selErr, context.Canceled) {
					return
				}
				p.Send(runErrMsg{err: selErr})
				return
			}
			runOpts.StartOptions.Containers = newContainers
			go resolveAndCacheLabel(ctx, runOpts.StartOptions, labelCh)
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

func resolveAndCacheLabel(ctx context.Context, opts container.StartOptions, labelCh chan<- string) {
	label, ok := container.ResolveEmulatorLabel(ctx, opts.PlatformClient, opts.Containers, opts.AuthToken, opts.Logger)
	if ok {
		config.CachePlanLabel(label)
	}
	labelCh <- label
}

func selectEmulatorInTUI(
	ctx context.Context,
	sink output.Sink,
	configPath string,
) ([]config.ContainerConfig, error) {
	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt: "Which emulator would you like to use?",
		Options: []output.InputOption{
			{Key: "a", Label: "AWS"},
			{Key: "s", Label: "Snowflake"},
		},
		ResponseCh: responseCh,
		Vertical:   true,
	})

	var resp output.InputResponse
	select {
	case resp = <-responseCh:
	case <-ctx.Done():
		return nil, context.Canceled
	}

	if resp.Cancelled {
		return nil, context.Canceled
	}

	selected := config.EmulatorAWS
	if resp.SelectedKey == "s" {
		selected = config.EmulatorSnowflake
	}

	if err := config.SwitchEmulator(selected); err != nil {
		return nil, fmt.Errorf("failed to switch emulator: %w", err)
	}
	newCfg, err := config.Get()
	if err != nil {
		return nil, err
	}

	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: selected.DisplayName() + " emulator selected."})
	if configPath != "" {
		sink.Emit(output.MessageEvent{Severity: output.SeveritySecondary, Text: "Change configuration in " + configPath + "."})
	}

	return newCfg.Containers, nil
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
