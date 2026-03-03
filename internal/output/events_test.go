package output

import "testing"

type captureSink struct {
	events []any
}

func (s *captureSink) emit(event any) {
	s.events = append(s.events, event)
}

func TestEmitAuth(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	EmitAuth(sink, AuthEvent{
		Preamble: "Welcome",
		Code:     "ABC123",
		URL:      "https://example.com",
	})

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	event, ok := sink.events[0].(AuthEvent)
	if !ok {
		t.Fatalf("expected AuthEvent, got %T", sink.events[0])
	}
	if event.Code != "ABC123" {
		t.Fatalf("expected code %q, got %q", "ABC123", event.Code)
	}
	if event.URL != "https://example.com" {
		t.Fatalf("expected URL %q, got %q", "https://example.com", event.URL)
	}
	if event.Preamble != "Welcome" {
		t.Fatalf("expected preamble %q, got %q", "Welcome", event.Preamble)
	}

	line, ok := FormatEventLine(event)
	if !ok {
		t.Fatal("expected formatter output")
	}
	if line != "Welcome\nYour one-time code: ABC123\nOpening browser to login...\nhttps://example.com" {
		t.Fatalf("unexpected formatted line: %q", line)
	}
}
