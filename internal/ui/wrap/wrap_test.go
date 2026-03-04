package wrap

import (
	"strings"
	"testing"
)

func TestHardWrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		width    int
		expected string
	}{
		{name: "short string unchanged", input: "hello", width: 10, expected: "hello"},
		{name: "exact width unchanged", input: "abcde", width: 5, expected: "abcde"},
		{name: "breaks at width", input: "abcdef", width: 3, expected: "abc\ndef"},
		{name: "utf8 rune aware", input: "A🙂BC", width: 2, expected: "A🙂\nBC"},
		{name: "zero width returns input", input: "abc", width: 0, expected: "abc"},
		{name: "negative width returns input", input: "abc", width: -1, expected: "abc"},
		{name: "empty string", input: "", width: 5, expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := HardWrap(tt.input, tt.width)
			if got != tt.expected {
				t.Fatalf("HardWrap(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.expected)
			}
		})
	}
}

func TestSoftWrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		width    int
		expected []string
	}{
		{
			name:     "short text unchanged",
			input:    "hello world",
			width:    20,
			expected: []string{"hello world"},
		},
		{
			name:     "wraps at word boundary",
			input:    "hello world foo",
			width:    11,
			expected: []string{"hello world", "foo"},
		},
		{
			name:     "long word hard-breaks",
			input:    "abcdefghij",
			width:    4,
			expected: []string{"abcd", "efgh", "ij"},
		},
		{
			name:     "long word followed by short word",
			input:    "abcdefgh xy",
			width:    4,
			expected: []string{"abcd", "efgh", "xy"},
		},
		{
			name:     "short word then long word",
			input:    "hi abcdefgh",
			width:    4,
			expected: []string{"hi", "abcd", "efgh"},
		},
		{
			name:     "long word exact multiple of width",
			input:    "abcdef",
			width:    3,
			expected: []string{"abc", "def"},
		},
		{
			name:     "utf8 rune aware split",
			input:    "🙂🙂🙂🙂🙂",
			width:    2,
			expected: []string{"🙂🙂", "🙂🙂", "🙂"},
		},
		{
			name:     "zero width returns input",
			input:    "abc",
			width:    0,
			expected: []string{"abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SoftWrap(tt.input, tt.width)
			if len(got) != len(tt.expected) {
				t.Fatalf("SoftWrap(%q, %d) returned %d lines %v, want %d lines %v",
					tt.input, tt.width, len(got), got, len(tt.expected), tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("SoftWrap(%q, %d)[%d] = %q, want %q",
						tt.input, tt.width, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSoftWrapNoLineExceedsMaxWidth(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"short",
		"a]very-long-token-without-spaces,here",
		"hello world this is a test of soft wrapping behavior",
		"🙂🙂🙂🙂🙂🙂🙂🙂🙂🙂",
	}

	for _, input := range inputs {
		for width := 1; width <= 10; width++ {
			lines := SoftWrap(input, width)
			for i, line := range lines {
				runes := []rune(line)
				if len(runes) > width {
					t.Errorf("SoftWrap(%q, %d): line %d has %d runes (%q), exceeds maxWidth",
						input, width, i, len(runes), line)
				}
			}
		}
	}
}

func TestSoftWrapPreservesContent(t *testing.T) {
	t.Parallel()

	input := "hello world foo bar"
	lines := SoftWrap(input, 7)
	got := strings.Join(lines, " ")
	if got != input {
		t.Fatalf("rejoined output %q != input %q", got, input)
	}
}
