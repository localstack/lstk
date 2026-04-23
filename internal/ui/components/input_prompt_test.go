package components

import (
	"strings"
	"testing"

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
				view := p.View()
				if view != "" {
					t.Fatalf("expected empty view when hidden, got: %q", view)
				}
				return
			}

			p = p.Show(tc.prompt, tc.options, tc.vertical)
			view := p.View()

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
