package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/localstack/lstk/internal/output"
)

func TestAppAddsFormattedLinesInOrder(t *testing.T) {
	tm := teatest.NewTestModel(t, NewApp("dev", nil), teatest.WithInitialTermSize(120, 40))
	tm.Send(output.LogEvent{Message: "first"})
	tm.Send(output.WarningEvent{Message: "second"})

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		view := string(bts)
		if !strings.Contains(view, "first") || !strings.Contains(view, "Warning: second") {
			return false
		}
		return strings.Index(view, "first") < strings.Index(view, "Warning: second")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
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
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd == nil {
		t.Fatal("expected response command")
	}
	cmd()

	// Verify response was sent
	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "enter" {
			t.Fatalf("expected enter key, got %q", resp.SelectedKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
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
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	cmd()

	// Verify cancellation response was sent
	select {
	case resp := <-responseCh:
		if !resp.Cancelled {
			t.Fatal("expected cancelled response")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
	}
	if !cancelled {
		t.Fatal("expected cancel callback")
	}
	if app.Err() != context.Canceled {
		t.Fatalf("expected context canceled error, got %v", app.Err())
	}
}

func TestAppEnterPrefersExplicitEnterOption(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt: "Open browser now?",
		Options: []output.InputOption{
			{Key: "y", Label: "Y"},
			{Key: "n", Label: "n"},
			{Key: "enter", Label: "Press ENTER when complete"},
		},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd == nil {
		t.Fatal("expected response command")
	}
	cmd()

	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "enter" {
			t.Fatalf("expected enter key, got %q", resp.SelectedKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
	}

	if app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be hidden after response")
	}
}

func TestAppEnterDoesNothingWithoutExplicitEnterOption(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt: "Open browser now?",
		Options: []output.InputOption{
			{Key: "y", Label: "Y"},
			{Key: "n", Label: "n"},
		},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatal("expected no response command when enter is not an explicit option")
	}

	select {
	case resp := <-responseCh:
		t.Fatalf("expected no response, got %+v", resp)
	case <-time.After(200 * time.Millisecond):
	}

	if !app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to remain visible")
	}
}
