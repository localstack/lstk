package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
)

func TestSpinner_StartStop(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	if s.Visible() {
		t.Fatal("expected spinner to be hidden initially")
	}

	s = s.Start("Loading", 0)
	if !s.Visible() {
		t.Fatal("expected spinner to be visible after Start")
	}

	s, _ = s.Stop()
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

	s = s.Start("Loading", 0)
	view := s.View()
	if !strings.Contains(view, "Loading") {
		t.Fatalf("expected view to contain 'Loading', got: %q", view)
	}
}

func TestSpinner_Update(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	s = s.Start("Loading", 0)

	_, cmd := s.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Fatal("expected non-nil command from spinner update")
	}
}

func TestSpinner_MinDuration_ImmediateStop(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	s = s.Start("Loading", 0)

	s, cmd := s.Stop()
	if s.Visible() {
		t.Fatal("expected spinner to be hidden immediately with 0 min duration")
	}
	if s.PendingStop() {
		t.Fatal("expected no pending stop with 0 min duration")
	}
	if cmd != nil {
		t.Fatal("expected no command with immediate stop")
	}
}

func TestSpinner_MinDuration_DelayedStop(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	s = s.Start("Loading", 500*time.Millisecond)

	s, cmd := s.Stop()
	if !s.Visible() {
		t.Fatal("expected spinner to remain visible during min duration")
	}
	if !s.PendingStop() {
		t.Fatal("expected pending stop during min duration")
	}
	if cmd == nil {
		t.Fatal("expected command for delayed stop")
	}
}

func TestSpinner_HandleMinDurationElapsed(t *testing.T) {
	t.Parallel()

	s := NewSpinner()
	s = s.Start("Loading", 500*time.Millisecond)
	s, _ = s.Stop()

	if !s.PendingStop() {
		t.Fatal("expected pending stop before handling elapsed")
	}

	s = s.HandleMinDurationElapsed()
	if s.Visible() {
		t.Fatal("expected spinner to be hidden after min duration elapsed")
	}
	if s.PendingStop() {
		t.Fatal("expected no pending stop after handling elapsed")
	}
}
