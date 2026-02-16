package ui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/localstack/lstk/internal/output"
)

func TestAppAddsFormattedLinesInOrder(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", nil)
	model, _ := app.Update(output.LogEvent{Message: "first"})
	app = model.(App)
	model, _ = app.Update(output.WarningEvent{Message: "second"})
	app = model.(App)

	view := app.View()
	if !strings.Contains(view, "first") || !strings.Contains(view, "Warning: second") {
		t.Fatalf("expected both lines in view, got: %q", view)
	}
	if strings.Index(view, "first") > strings.Index(view, "Warning: second") {
		t.Fatalf("messages are out of order: %q", view)
	}
}

func TestAppBoundsMessageHistory(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", nil)
	for i := 0; i < maxLines+5; i++ {
		model, _ := app.Update(output.LogEvent{Message: "line"})
		app = model.(App)
	}
	if len(app.lines) != maxLines {
		t.Fatalf("expected %d lines, got %d", maxLines, len(app.lines))
	}
}

func TestAppQuitCancelsContext(t *testing.T) {
	t.Parallel()

	cancelled := false
	app := NewApp("dev", func() { cancelled = true })
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	app = model.(App)

	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if !cancelled {
		t.Fatal("expected cancel callback")
	}
	if app.Err() != context.Canceled {
		t.Fatalf("expected context canceled error, got %v", app.Err())
	}
}
