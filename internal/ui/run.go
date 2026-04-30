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
	LabelCh                <-chan string
	NeedsEmulatorSelection bool
	// OnEmulatorSelected is called with the user's choice when NeedsEmulatorSelection is true.
	// It should switch the config and return the updated container configs to use for this run.
	OnEmulatorSelected func(config.EmulatorType) ([]config.ContainerConfig, error)
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
		if runOpts.NeedsEmulatorSelection {
			newContainers, selErr := selectEmulatorInTUI(ctx, sink, runOpts.ConfigPath, runOpts.OnEmulatorSelected)
			if selErr != nil {
				if errors.Is(selErr, context.Canceled) {
					return
				}
				p.Send(runErrMsg{err: selErr})
				return
			}
			runOpts.StartOptions.Containers = newContainers
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

func selectEmulatorInTUI(
	ctx context.Context,
	sink output.Sink,
	configPath string,
	onSelected func(config.EmulatorType) ([]config.ContainerConfig, error),
) ([]config.ContainerConfig, error) {
	responseCh := make(chan output.InputResponse, 1)
	sink.Emit(output.UserInputRequestEvent{
		Prompt: "Which emulator would you like to use?",
		Options: []output.InputOption{
			{Key: "a", Label: "AWS [A]"},
			{Key: "s", Label: "Snowflake [S]"},
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

	containers, err := onSelected(selected)
	if err != nil {
		return nil, err
	}

	msg := selected.DisplayName() + " emulator selected."
	if configPath != "" {
		msg += fmt.Sprintf(" You can change this anytime in %s.", configPath)
	}
	sink.Emit(output.MessageEvent{Severity: output.SeverityNote, Text: msg})

	return containers, nil
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
