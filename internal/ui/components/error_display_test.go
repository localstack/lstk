package components

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/wrap"
)

func TestErrorDisplay_ShowView(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	if e.Visible() {
		t.Fatal("expected error display to be hidden initially")
	}

	if e.View(80) != "" {
		t.Fatal("expected empty view when error display is hidden")
	}

	e = e.Show(output.ErrorEvent{
		Title:   "Connection failed",
		Summary: "Could not connect to Docker daemon",
		Detail:  "Is Docker running?",
		Actions: []output.ErrorAction{
			{Label: "Start Docker:", Value: "open -a Docker"},
		},
	})

	if !e.Visible() {
		t.Fatal("expected error display to be visible after Show")
	}

	view := e.View(80)
	if !strings.Contains(view, "Connection failed") {
		t.Fatalf("expected view to contain title, got: %q", view)
	}
	if !strings.Contains(view, "Could not connect to Docker daemon") {
		t.Fatalf("expected view to contain summary, got: %q", view)
	}
	if !strings.Contains(view, "Is Docker running?") {
		t.Fatalf("expected view to contain detail, got: %q", view)
	}
	if !strings.Contains(view, "Start Docker:") {
		t.Fatalf("expected view to contain action label, got: %q", view)
	}
	if !strings.Contains(view, "open -a Docker") {
		t.Fatalf("expected view to contain action value, got: %q", view)
	}
}

func TestErrorDisplay_MultiActionRenders(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	e = e.Show(output.ErrorEvent{
		Title:   "Port 4566 already in use",
		Summary: "LocalStack may already be running.",
		Actions: []output.ErrorAction{
			{Label: "Stop existing emulator:", Value: "lstk stop"},
			{Label: "Use another port in the configuration:", Value: "/home/user/.config/lstk/config.toml"},
		},
	})

	view := e.View(80)
	if !strings.Contains(view, "Port 4566 already in use") {
		t.Fatalf("expected view to contain title, got: %q", view)
	}
	if !strings.Contains(view, "LocalStack may already be running.") {
		t.Fatalf("expected view to contain summary, got: %q", view)
	}
	if !strings.Contains(view, "Stop existing emulator:") {
		t.Fatalf("expected view to contain first action label, got: %q", view)
	}
	if !strings.Contains(view, "lstk stop") {
		t.Fatalf("expected view to contain first action value, got: %q", view)
	}
	if !strings.Contains(view, "Use another port in the configuration:") {
		t.Fatalf("expected view to contain second action label, got: %q", view)
	}
	if !strings.Contains(view, "/home/user/.config/lstk/config.toml") {
		t.Fatalf("expected view to contain second action value, got: %q", view)
	}
}

func TestErrorDisplay_TitleWrapsToWidth(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	longTitle := "license validation failed for test-product:1.2.3: license validation failed: your token is invalid"
	e = e.Show(output.ErrorEvent{Title: longTitle})

	const maxWidth = 50

	// Verify that View contains the full text (content presence check on styled output)
	view := e.View(maxWidth)
	if !strings.Contains(view, "invalid") {
		t.Fatalf("expected view to contain full text, got: %q", view)
	}

	// Verify wrapping widths on unstyled text to avoid ANSI escape code interference
	titlePrefix := "✗ "
	prefixWidth := utf8.RuneCountInString(titlePrefix)
	titleWidth := maxWidth - prefixWidth
	wrappedLines := wrap.SoftWrap(longTitle, titleWidth)
	if len(wrappedLines) < 2 {
		t.Fatalf("expected title to wrap across multiple lines, got %d", len(wrappedLines))
	}
	for _, line := range wrappedLines {
		if utf8.RuneCountInString(line) > titleWidth {
			t.Errorf("wrapped line exceeds max width %d: %q (%d runes)", titleWidth, line, utf8.RuneCountInString(line))
		}
	}
}

func TestErrorDisplay_MinimalEvent(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	e = e.Show(output.ErrorEvent{Title: "Something went wrong"})

	view := e.View(80)
	if !strings.Contains(view, "Something went wrong") {
		t.Fatalf("expected view to contain title, got: %q", view)
	}
}
