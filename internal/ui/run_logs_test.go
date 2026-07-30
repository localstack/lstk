package ui

import (
	"context"
	"testing"
	"time"

	"github.com/localstack/lstk/internal/output"
)

// blockingPrintlner simulates a Program that stopped draining Println,
// blocking forever like the real unbuffered channel send would.
type blockingPrintlner struct{}

func (blockingPrintlner) Println(args ...interface{}) {
	select {}
}

func TestProgramLogPrinterReturnsPromptlyWhenCtxCancelledMidPrintln(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	printer := programLogPrinter{p: blockingPrintlner{}, ctx: ctx}

	done := make(chan struct{})
	go func() {
		printer.PrintLogLine(output.LogLineEvent{Source: "emulator", Line: "hangs forever"})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("PrintLogLine did not return after ctx was cancelled; it would hang RunLogs forever")
	}
}

type recordingPrintlner struct {
	lines chan string
}

func (r recordingPrintlner) Println(args ...interface{}) {
	r.lines <- args[0].(string)
}

func TestProgramLogPrinterWaitsForPrintlnWhenNotCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	recorder := recordingPrintlner{lines: make(chan string, 1)}
	printer := programLogPrinter{p: recorder, ctx: ctx}
	printer.PrintLogLine(output.LogLineEvent{Source: "emulator", Line: "hello", Level: output.LogLevelInfo})

	select {
	case line := <-recorder.lines:
		if line == "" {
			t.Fatal("expected rendered log line to be printed")
		}
	default:
		t.Fatal("expected PrintLogLine to have already delivered the line to Println")
	}
}
