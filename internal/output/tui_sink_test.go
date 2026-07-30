package output

import (
	"reflect"
	"testing"
)

type testSender struct {
	msgs []any
}

func (s *testSender) Send(msg any) {
	s.msgs = append(s.msgs, msg)
}

func TestTUISinkForwardsEvents(t *testing.T) {
	t.Parallel()

	sender := &testSender{}
	sink := NewTUISink(sender)

	sink.Emit(MessageEvent{Severity: SeverityInfo, Text: "hello"})
	sink.Emit(MessageEvent{Severity: SeverityWarning, Text: "careful"})
	sink.Emit(ContainerStatusEvent{Phase: "starting", Container: "localstack"})
	sink.Emit(ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2})
	sink.Emit(AuthCompleteEvent{})

	want := []any{
		MessageEvent{Severity: SeverityInfo, Text: "hello"},
		MessageEvent{Severity: SeverityWarning, Text: "careful"},
		ContainerStatusEvent{Phase: "starting", Container: "localstack"},
		ProgressEvent{LayerID: "abc", Status: "Downloading", Current: 1, Total: 2},
		AuthCompleteEvent{},
	}
	if !reflect.DeepEqual(sender.msgs, want) {
		t.Fatalf("unexpected msgs: got=%#v want=%#v", sender.msgs, want)
	}
}

func TestTUISinkNilSenderNoPanic(t *testing.T) {
	t.Parallel()

	sink := NewTUISink(nil)
	sink.Emit(MessageEvent{Severity: SeverityInfo, Text: "noop"})
}

type testLogPrinter struct {
	lines []LogLineEvent
}

func (p *testLogPrinter) PrintLogLine(event LogLineEvent) {
	p.lines = append(p.lines, event)
}

// A streaming log sink routes LogLineEvent to the log printer and every
// other event to the sender, as before.
func TestStreamingLogSinkRoutesLogLinesToPrinter(t *testing.T) {
	t.Parallel()

	sender := &testSender{}
	printer := &testLogPrinter{}
	sink := NewStreamingLogSink(sender, printer)

	sink.Emit(LogLineEvent{Source: "emulator", Line: "first", Level: LogLevelInfo})
	sink.Emit(MessageEvent{Severity: SeverityWarning, Text: "careful"})
	sink.Emit(LogLineEvent{Source: "emulator", Line: "second", Level: LogLevelInfo})

	wantLines := []LogLineEvent{
		{Source: "emulator", Line: "first", Level: LogLevelInfo},
		{Source: "emulator", Line: "second", Level: LogLevelInfo},
	}
	if !reflect.DeepEqual(printer.lines, wantLines) {
		t.Fatalf("unexpected printed lines: got=%#v want=%#v", printer.lines, wantLines)
	}

	wantSent := []any{MessageEvent{Severity: SeverityWarning, Text: "careful"}}
	if !reflect.DeepEqual(sender.msgs, wantSent) {
		t.Fatalf("unexpected sent msgs: got=%#v want=%#v", sender.msgs, wantSent)
	}
}

func TestStreamingLogSinkNilLogPrinterFallsBackToSender(t *testing.T) {
	t.Parallel()

	sender := &testSender{}
	sink := NewStreamingLogSink(sender, nil)

	sink.Emit(LogLineEvent{Source: "emulator", Line: "first", Level: LogLevelInfo})

	want := []any{LogLineEvent{Source: "emulator", Line: "first", Level: LogLevelInfo}}
	if !reflect.DeepEqual(sender.msgs, want) {
		t.Fatalf("unexpected sent msgs: got=%#v want=%#v", sender.msgs, want)
	}
}
