package cmd

import (
	"strings"
	"testing"
)

func TestWrapLine(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		width int
		want  string
	}{
		{
			name:  "wraps at word boundary",
			line:  "Host environment variables prefixed with LOCALSTACK_ are forwarded.",
			width: 30,
			want:  "Host environment variables\nprefixed with LOCALSTACK_ are\nforwarded.",
		},
		{
			name:  "short line is untouched",
			line:  "Start emulator and services.",
			width: 80,
			want:  "Start emulator and services.",
		},
		{
			name:  "word longer than width stays on its own line",
			line:  "a supercalifragilisticexpialidocious word",
			width: 10,
			want:  "a\nsupercalifragilisticexpialidocious\nword",
		},
		{
			name:  "empty line stays empty",
			line:  "",
			width: 80,
			want:  "",
		},
		{
			name:  "indented example is left untouched even when over width",
			line:  "  lstk snapshot save     # saves to ./snapshot.zip",
			width: 20,
			want:  "  lstk snapshot save     # saves to ./snapshot.zip",
		},
		{
			name:  "tab-indented line is left untouched even when over width",
			line:  "\tlstk az group list extra words past the width",
			width: 10,
			want:  "\tlstk az group list extra words past the width",
		},
		{
			name:  "length equal to width is left untouched",
			line:  "ab cd",
			width: 5,
			want:  "ab cd",
		},
		{
			name:  "wraps when one rune over width",
			line:  "ab cde",
			width: 5,
			want:  "ab\ncde",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wrapLine(tt.line, tt.width); got != tt.want {
				t.Fatalf("wrapLine(%q, %d)\n got: %q\nwant: %q", tt.line, tt.width, got, tt.want)
			}
		})
	}
}

func TestWrapTextPreservesBlankLines(t *testing.T) {
	// Width-independent so the test never depends on the ambient terminal size.
	in := "Start emulator and services.\n\nHost variables are forwarded."
	got := wrapText(in)
	if strings.Count(got, "\n\n") != strings.Count(in, "\n\n") {
		t.Fatalf("wrapText changed the blank-line structure\n got: %q\nin:   %q", got, in)
	}
}
