package components

import (
	"strings"
	"testing"

	"github.com/localstack/lstk/internal/output"
)

func TestErrorDisplay_ShowView(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	if e.Visible() {
		t.Fatal("expected error display to be hidden initially")
	}

	if e.View() != "" {
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

	view := e.View()
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

func TestErrorDisplay_MinimalEvent(t *testing.T) {
	t.Parallel()

	e := NewErrorDisplay()
	e = e.Show(output.ErrorEvent{Title: "Something went wrong"})

	view := e.View()
	if !strings.Contains(view, "Something went wrong") {
		t.Fatalf("expected view to contain title, got: %q", view)
	}
}
