package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/localstack/lstk/internal/output"
)

func TestInputPromptView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prompt   string
		options  []output.InputOption
		vertical bool
		contains []string
		excludes []string
	}{
		{
			name:     "hidden returns empty",
			prompt:   "",
			options:  nil,
			vertical: false,
			contains: nil,
		},
		{
			name:   "no options",
			prompt: "Continue?",
			options: nil,
			vertical: false,
			contains: []string{"?", "Continue?"},
			excludes: []string{"(", "["},
		},
		{
			name:   "single option shows parentheses",
			prompt: "Continue?",
			options: []output.InputOption{{Key: "enter", Label: "Press ENTER"}},
			vertical: false,
			contains: []string{"?", "Continue?", "(Press ENTER)"},
		},
		{
			name:   "multiple options shows brackets",
			prompt: "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?",
			options: []output.InputOption{
				{Key: "y", Label: "Y"},
				{Key: "n", Label: "n"},
			},
			vertical: false,
			contains: []string{"?", "Set up a LocalStack profile for AWS CLI and SDKs in ~/.aws?", "[Y/n]"},
		},
		{
			name:   "multi-line prompt renders trailing lines",
			prompt: "First line\nSecond line\nThird line",
			options: []output.InputOption{{Key: "y", Label: "Y"}},
			vertical: false,
			contains: []string{"?", "First line", "Second line", "Third line", "(Y)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := NewInputPrompt()

			if tc.prompt == "" && tc.options == nil {
				view := p.View(0)
				if view != "" {
					t.Fatalf("expected empty view when hidden, got: %q", view)
				}
				return
			}

			p = p.Show(tc.prompt, tc.options, tc.vertical)
			view := p.View(0)

			for _, s := range tc.contains {
				if !strings.Contains(view, s) {
					t.Errorf("expected view to contain %q, got: %q", s, view)
				}
			}
			for _, s := range tc.excludes {
				if strings.Contains(view, s) {
					t.Errorf("expected view NOT to contain %q, got: %q", s, view)
				}
			}
		})
	}
}

// TestInputPromptViewWrapsWithoutLosingKeyHints covers DEVX-1045: the license
// re-login prompt is long enough that Bubble Tea's renderer truncated the key
// hints off the right edge, leaving a question with no visible answer.
func TestInputPromptViewWrapsWithoutLosingKeyHints(t *testing.T) {
	t.Parallel()

	const width = 40
	question := "License validation failed: invalid, inactive, or expired authentication token or subscription. Log in again to refresh your credentials?"
	p := NewInputPrompt().Show(question, []output.InputOption{
		{Key: "enter", Label: "ENTER to log in again"},
		{Key: "esc", Label: "ESC to exit"},
	}, false)

	view := p.View(width)

	if !strings.Contains(view, "[ENTER to log in again/ESC to exit]") {
		t.Errorf("expected the key hints to survive wrapping intact, got:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line exceeds width %d (%d): %q", width, w, line)
		}
	}
	// The question must still be readable end to end once the wrap points and
	// the continuation indent are removed.
	flattened := strings.Join(strings.Fields(view), " ")
	if !strings.Contains(flattened, strings.Join(strings.Fields(question), " ")) {
		t.Errorf("expected the whole question to survive wrapping, got:\n%s", view)
	}
}

func TestInputPromptViewUnwrappedWithoutWidth(t *testing.T) {
	t.Parallel()

	p := NewInputPrompt().Show("Continue?", []output.InputOption{{Key: "enter", Label: "Press ENTER"}}, false)

	view := p.View(0)
	if !strings.Contains(view, "Continue? (Press ENTER)") {
		t.Errorf("expected an unwrapped single line before the first WindowSizeMsg, got: %q", view)
	}
	if strings.Contains(view, "\n") {
		t.Errorf("expected no wrapping without a known width, got: %q", view)
	}
}

func TestInputPromptViewSlowStartChoicesAreScannable(t *testing.T) {
	t.Parallel()

	const width = 80
	question := "LocalStack is still starting. Check progress with 'lstk logs'."
	p := NewInputPrompt().Show(question, []output.InputOption{
		{Key: "w", Label: "[W] Keep waiting"},
		{Key: "s", Label: "[S] Stop and exit"},
	}, true)

	view := p.View(width)
	lines := strings.Split(view, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a question and two vertical choices, got:\n%s", view)
	}
	if !strings.Contains(lines[0], question) {
		t.Fatalf("expected the question and log command on one line, got:\n%s", view)
	}
	if !strings.Contains(lines[1], "[W] Keep waiting") || !strings.Contains(lines[2], "[S] Stop and exit") {
		t.Fatalf("expected prefixed shortcuts on separate lines, got:\n%s", view)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			t.Fatalf("line exceeds width %d: %q", width, line)
		}
	}
}

// TestInputPromptViewReloginChoicesAreScannable covers the license re-login
// prompt: its question is long enough to wrap, so flattening the two choices
// into a trailing hint made them read as prose. They belong on their own lines
// below the wrapped question, shortcut first.
func TestInputPromptViewReloginChoicesAreScannable(t *testing.T) {
	t.Parallel()

	const width = 40
	question := "License validation failed: invalid, inactive, or expired authentication token or subscription."
	p := NewInputPrompt().Show(question, []output.InputOption{
		{Key: "r", Label: "[R] Re-authenticate"},
		{Key: "esc", Label: "[ESC] Exit"},
	}, true)

	view := p.View(width)
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a wrapped question and two vertical choices, got:\n%s", view)
	}
	if choices := lines[len(lines)-2:]; !strings.Contains(choices[0], "[R] Re-authenticate") ||
		!strings.Contains(choices[1], "[ESC] Exit") {
		t.Fatalf("expected each choice on its own trailing line, got:\n%s", view)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line exceeds width %d (%d): %q", width, w, line)
		}
	}
	flattened := strings.Join(strings.Fields(view), " ")
	if !strings.Contains(flattened, strings.Join(strings.Fields(question), " ")) {
		t.Errorf("expected the whole question to survive wrapping, got:\n%s", view)
	}
}
