package output

import "testing"

type captureSink struct {
	events []any
}

func (s *captureSink) emit(event any) {
	s.events = append(s.events, event)
}

func TestEmitHighlightLog(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	EmitHighlightLog(sink, "important")

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	event, ok := sink.events[0].(HighlightLogEvent)
	if !ok {
		t.Fatalf("expected HighlightLogEvent, got %T", sink.events[0])
	}
	if event.Message != "important" {
		t.Fatalf("expected message %q, got %q", "important", event.Message)
	}

	line, ok := FormatEventLine(event)
	if !ok {
		t.Fatal("expected formatter output")
	}
	if line != "important" {
		t.Fatalf("expected formatted line %q, got %q", "important", line)
	}
}

func TestEmitSecondaryLog(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	EmitSecondaryLog(sink, "tip")

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	event, ok := sink.events[0].(SecondaryLogEvent)
	if !ok {
		t.Fatalf("expected SecondaryLogEvent, got %T", sink.events[0])
	}
	if event.Message != "tip" {
		t.Fatalf("expected message %q, got %q", "tip", event.Message)
	}

	line, ok := FormatEventLine(event)
	if !ok {
		t.Fatal("expected formatter output")
	}
	if line != "tip" {
		t.Fatalf("expected formatted line %q, got %q", "tip", line)
	}
}
