package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestSpinner_StartStop(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	if s.Visible() {
		t.Fatal("expected spinner to be hidden initially")
	}

	s = s.Start("Loading")
	if !s.Visible() {
		t.Fatal("expected spinner to be visible after Start")
	}

	s = s.Stop()
	if s.Visible() {
		t.Fatal("expected spinner to be hidden after Stop")
	}
}

func TestSpinner_View(t *testing.T) {
	t.Parallel()

	s := NewSpinner()

	if s.View() != "" {
		t.Fatal("expected empty view when spinner is hidden")
	}

	s = s.Start("Loading")
	view := s.View()
	if !strings.Contains(view, "Loading") {
		t.Fatalf("expected view to contain 'Loading', got: %q", view)
	}
}

func TestSpinner_Update(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	s = s.Start("Loading")

	s, cmd := s.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil command from spinner update")
	}
}
