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

// printlner lets tests substitute a fake *tea.Program.
type printlner interface {
	Println(args ...interface{})
}

// programLogPrinter prints log lines permanently above the Program instead of
// the App model's capped a.lines buffer. Must be called synchronously from a
// single goroutine, not returned as a tea.Cmd — Cmds run concurrently and
// would reorder lines.
type programLogPrinter struct {
	p   printlner
	ctx context.Context
}

// Println, unlike Send, has no escape valve for a Program that already
// stopped reading its message channel (e.g. it quit, or a signal bypassed
// Update) — it would then block forever. Racing it against ctx.Done avoids
// wedging the caller; the abandoned goroutine only leaks in that shutdown
// race, and dies with the process.
func (l programLogPrinter) PrintLogLine(event output.LogLineEvent) {
	done := make(chan struct{})
	go func() {
		l.p.Println(renderLogLineEvent(event, 0))
		close(done)
	}()
	select {
	case <-done:
	case <-l.ctx.Done():
	}
}

func RunLogs(parentCtx context.Context, rt runtime.Runtime, containers []config.ContainerConfig, follow bool, tail string, verbose bool) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	app := NewApp("", "", "", cancel, withoutHeader())
	p := tea.NewProgram(app, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	runErrCh := make(chan error, 1)

	go func() {
		sink := output.NewStreamingLogSink(programSender{p: p}, programLogPrinter{p: p, ctx: ctx})
		err := container.Logs(ctx, rt, sink, containers, follow, tail, verbose)
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
