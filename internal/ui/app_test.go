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
	tm := teatest.NewTestModel(t, NewApp("dev", "", "", nil), teatest.WithInitialTermSize(120, 40))
	tm.Send(output.MessageEvent{Severity: output.SeverityInfo, Text: "first"})
	tm.Send(output.MessageEvent{Severity: output.SeverityWarning, Text: "second"})

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		view := string(bts)
		if !strings.Contains(view, "first") || !strings.Contains(view, "> Warning: second") {
			return false
		}
		return strings.Index(view, "first") < strings.Index(view, "> Warning: second")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.QuitMsg{})
	tm.WaitFinished(t, teatest.WithFinalTimeout(time.Second))
}

func TestAppBoundsMessageHistory(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
	for i := 0; i < maxLines+5; i++ {
		model, _ := app.Update(output.MessageEvent{Severity: output.SeverityInfo, Text: "line"})
		app = model.(App)
	}
	if len(app.lines) != maxLines {
		t.Fatalf("expected %d lines, got %d", maxLines, len(app.lines))
	}
}

func TestAppQuitCancelsContext(t *testing.T) {
	t.Parallel()

	cancelled := false
	app := NewApp("dev", "", "", func() { cancelled = true })
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

	app := NewApp("dev", "", "", nil)

	responseCh := make(chan output.InputResponse, 1)
	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt:     "Press enter",
		Options:    []output.InputOption{{Key: "enter", Label: "Continue"}},
		ResponseCh: responseCh,
	})
	app = model.(App)

	if !app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be visible")
	}

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

func TestAppCtrlCCancelsPendingInput(t *testing.T) {
	t.Parallel()

	cancelled := false
	app := NewApp("dev", "", "", func() { cancelled = true })

	responseCh := make(chan output.InputResponse, 1)
	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt:     "Press enter",
		Options:    []output.InputOption{{Key: "enter", Label: "Continue"}},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	app = model.(App)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	cmd()

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

func TestAppSpinnerStartStop(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	if app.spinner.Visible() {
		t.Fatal("expected spinner to be hidden initially")
	}

	model, cmd := app.Update(output.SpinnerEvent{Active: true, Text: "Loading"})
	app = model.(App)

	if !app.spinner.Visible() {
		t.Fatal("expected spinner to be visible after SpinnerEvent start")
	}
	if cmd == nil {
		t.Fatal("expected tick command after spinner start")
	}

	model, _ = app.Update(output.SpinnerEvent{Active: false})
	app = model.(App)

	if app.spinner.Visible() {
		t.Fatal("expected spinner to be hidden after SpinnerEvent stop")
	}
}

func TestAppMessageEventRendering(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, _ := app.Update(output.MessageEvent{Severity: output.SeveritySuccess, Text: "Done"})
	app = model.(App)

	if len(app.lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(app.lines))
	}
	if !strings.Contains(app.lines[0].text, "Success:") || !strings.Contains(app.lines[0].text, "Done") {
		t.Fatalf("expected rendered success message, got: %q", app.lines[0].text)
	}
}

func TestAppErrorEventStopsSpinner(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, _ := app.Update(output.SpinnerEvent{Active: true, Text: "Loading"})
	app = model.(App)

	if !app.spinner.Visible() {
		t.Fatal("expected spinner to be visible")
	}

	model, _ = app.Update(output.ErrorEvent{Title: "Something went wrong"})
	app = model.(App)

	if app.spinner.Visible() {
		t.Fatal("expected spinner to be stopped after ErrorEvent")
	}
	if !app.errorDisplay.Visible() {
		t.Fatal("expected error display to be visible after ErrorEvent")
	}
}

func TestAppEnterPrefersExplicitEnterOption(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
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

func TestAppEnterSelectsUppercaseLabelDefault(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
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
	if cmd == nil {
		t.Fatal("expected response command when enter is pressed with uppercase default")
	}
	cmd()

	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "y" {
			t.Fatalf("expected y key, got %q", resp.SelectedKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
	}

	if app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be hidden after response")
	}
}

func TestAppEnterDoesNothingWithoutDefault(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt: "Choose:",
		Options: []output.InputOption{
			{Key: "y", Label: "y"},
			{Key: "n", Label: "n"},
		},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatal("expected no response command when no uppercase default option exists")
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

func TestAppEnterDoesNothingWithNonLetterLabel(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt: "Choose:",
		Options: []output.InputOption{
			{Key: "1", Label: "1"},
			{Key: "2", Label: "2"},
		},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = model.(App)
	if cmd != nil {
		t.Fatal("expected no response command when label contains no letters")
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

func TestAppAnyKeyOptionResolvesOnAnyKeypress(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.UserInputRequestEvent{
		Prompt:     "Waiting for authorization...",
		Options:    []output.InputOption{{Key: "any", Label: "Press any key when complete"}},
		ResponseCh: responseCh,
	})
	app = model.(App)

	// Any key (e.g., spacebar) should resolve
	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	app = model.(App)
	if cmd == nil {
		t.Fatal("expected response command")
	}
	cmd()

	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "any" {
			t.Fatalf("expected any key, got %q", resp.SelectedKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
	}

	if app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be hidden after response")
	}
}

func TestAppPullProgressShowsOnPullingPhase(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, _ := app.Update(output.ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack-pro:latest"})
	app = model.(App)

	if !app.pullProgress.Visible() {
		t.Fatal("expected pull progress to be visible during pulling phase")
	}
	if len(app.lines) != 0 {
		t.Fatalf("expected no lines appended for pulling phase, got %d", len(app.lines))
	}
}

func TestAppPullProgressHidesOnNextPhase(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, _ := app.Update(output.ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack-pro:latest"})
	app = model.(App)

	model, _ = app.Update(output.ContainerStatusEvent{Phase: "starting", Container: "localstack"})
	app = model.(App)

	if app.pullProgress.Visible() {
		t.Fatal("expected pull progress to be hidden after pulling phase ends")
	}
	if len(app.lines) != 1 {
		t.Fatalf("expected 1 line for starting phase, got %d", len(app.lines))
	}
}

func TestAppProgressEventUpdatesPullProgress(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, _ := app.Update(output.ContainerStatusEvent{Phase: "pulling", Container: "localstack/localstack-pro:latest"})
	app = model.(App)

	model, _ = app.Update(output.ProgressEvent{
		Container: "localstack/localstack-pro:latest",
		LayerID:   "abc123",
		Status:    "Downloading",
		Current:   50,
		Total:     100,
	})
	app = model.(App)

	if len(app.lines) != 0 {
		t.Fatalf("expected no lines appended for progress event, got %d", len(app.lines))
	}

	view := app.pullProgress.View()
	if !strings.Contains(view, "layers") {
		t.Fatalf("expected pull progress view to show layer count, got: %q", view)
	}
}

func TestAppProgressEventIgnoredWhenNotPulling(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)

	model, cmd := app.Update(output.ProgressEvent{
		Container: "localstack/localstack-pro:latest",
		LayerID:   "abc123",
		Status:    "Downloading",
		Current:   50,
		Total:     100,
	})
	app = model.(App)

	if cmd != nil {
		t.Fatal("expected no command when pull progress is not visible")
	}
	if len(app.lines) != 0 {
		t.Fatalf("expected no lines appended, got %d", len(app.lines))
	}
}

func TestAppPendingInputOptionCOverridesClipboardShortcut(t *testing.T) {
	t.Parallel()

	app := NewApp("dev", "", "", nil)
	responseCh := make(chan output.InputResponse, 1)

	model, _ := app.Update(output.AuthEvent{URL: "https://example.com"})
	app = model.(App)

	model, _ = app.Update(output.UserInputRequestEvent{
		Prompt: "Choose option",
		Options: []output.InputOption{
			{Key: "c", Label: "Continue"},
			{Key: "x", Label: "Cancel"},
		},
		ResponseCh: responseCh,
	})
	app = model.(App)

	model, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	app = model.(App)
	if cmd == nil {
		t.Fatal("expected pending-input response command")
	}
	msg := cmd()
	if msg != nil {
		t.Fatalf("expected pending-input command to return nil tea.Msg, got %#v", msg)
	}

	select {
	case resp := <-responseCh:
		if resp.SelectedKey != "c" {
			t.Fatalf("expected c key, got %q", resp.SelectedKey)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response on channel")
	}

	if app.inputPrompt.Visible() {
		t.Fatal("expected input prompt to be hidden after response")
	}
}
