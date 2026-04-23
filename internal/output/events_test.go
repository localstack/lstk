package output

import "testing"

type captureSink struct {
	events []Event
}

func (s *captureSink) Emit(event Event) {
	s.events = append(s.events, event)
}

func TestEmitAuth(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	sink.Emit(AuthEvent{
		Preamble: "Welcome",
		URL:      "https://example.com",
	})

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	event, ok := sink.events[0].(AuthEvent)
	if !ok {
		t.Fatalf("expected AuthEvent, got %T", sink.events[0])
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
	expected := "Welcome\nOpening browser to login...\nBrowser didn't open? Visit https://example.com"
	if line != expected {
		t.Fatalf("unexpected formatted line: %q", line)
	}
}

func TestEmitAuthWithCode(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	sink.Emit(AuthEvent{
		Preamble: "Welcome",
		URL:      "https://example.com",
		Code:     "1234",
	})

	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	event, ok := sink.events[0].(AuthEvent)
	if !ok {
		t.Fatalf("expected AuthEvent, got %T", sink.events[0])
	}
	if event.Preamble != "Welcome" {
		t.Fatalf("expected preamble %q, got %q", "Welcome", event.Preamble)
	}
	if event.URL != "https://example.com" {
		t.Fatalf("expected URL %q, got %q", "https://example.com", event.URL)
	}
	if event.Code != "1234" {
		t.Fatalf("expected code %q, got %q", "1234", event.Code)
	}

	line, ok := FormatEventLine(event)
	if !ok {
		t.Fatal("expected formatter output")
	}
	expected := "Welcome\nOpening browser to login...\nBrowser didn't open? Visit https://example.com\n\nOne-time code: 1234"
	if line != expected {
		t.Fatalf("unexpected formatted line: %q", line)
	}
}
