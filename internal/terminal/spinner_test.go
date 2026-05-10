package terminal

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestSpinnerSilentWhenStoppedBeforeDelay(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	s := NewSpinner(&out, "loading", 200*time.Millisecond)
	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	if got := out.String(); got != "" {
		t.Fatalf("expected no output when stopped before delay, got %q", got)
	}
}

func TestSpinnerRendersAfterDelay(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	s := NewSpinner(&out, "loading", 30*time.Millisecond)
	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	if got := out.String(); !strings.Contains(got, "loading") {
		t.Fatalf("expected label %q in output, got %q", "loading", got)
	}
}

func TestSpinnerRendersImmediatelyWithZeroDelay(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	s := NewSpinner(&out, "loading", 0)
	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Stop()

	if got := out.String(); !strings.Contains(got, "loading") {
		t.Fatalf("expected label %q in output, got %q", "loading", got)
	}
}