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

func TestAppEnterRespondsToInputRequest(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", nil)

	// First, send a user input request
	responseCh := make(chan output.InputResponse, 1)
	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt:     "Press enter",
		Options:    []output.InputOption{{Key: "enter", Label: "Continue"}},
		ResponseCh: responseCh,
	})
	app = model.(App)

	// Verify input prompt is visible
	if !app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be visible")
	}

	// Now send ENTER key
	model, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)

	// Verify response was sent
	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "enter" {
			t.Fatalf("expected enter key, got %q", resp.SelectedKey)
		}
	default:
		t.Fatal("expected response on channel")
	}

	// Verify input prompt is hidden
	if app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be hidden after response")
	}
}

func TestAppCtrlCCancelsPendingInput(t *testing.T) {
	t.Parallel()

	cancelled := false
	app := NewApp("dev", func() { cancelled = true })

	// Send a user input request
	responseCh := make(chan output.InputResponse, 1)
	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt:     "Press enter",
		Options:    []output.InputOption{{Key: "enter", Label: "Continue"}},
		ResponseCh: responseCh,
	})
	app = model.(App)

	// Send Ctrl+C
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	app = model.(App)

	// Verify cancellation response was sent
	select {
	case resp := <-responseCh:
		if !resp.Cancelled {
			t.Fatal("expected cancelled response")
		}
	default:
		t.Fatal("expected response on channel")
	}

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
