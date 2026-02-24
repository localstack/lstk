package cmd

import (
	"strings"
	"testing"
)

func TestVersionLine(t *testing.T) {
	got := versionLine()

	if !strings.HasPrefix(got, "lstk ") {
		t.Fatalf("versionLine() = %q, should start with 'lstk '", got)
	}
	if !strings.Contains(got, "(") || !strings.Contains(got, ")") {
		t.Fatalf("versionLine() = %q, should contain parentheses with commit and date", got)
	}
}
