package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/ui/styles"
	"github.com/localstack/lstk/internal/ui/wrap"
	"github.com/muesli/termenv"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func withTrueColorProfile(t *testing.T) {
	t.Helper()

	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })
}

func stripANSI(s string) string {
	return ansiRegexp.ReplaceAllString(s, "")
}

func TestRenderLogLineWrapsPlainTextAtAvailableWidth(t *testing.T) {
	t.Parallel()
	withTrueColorProfile(t)

	got := stripANSI(renderLogLine("abcdefghij", output.LogLevelInfo, 4, 0))
	want := "abcd\nefgh\nij"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRenderLogLineUsesVisiblePrefixWidth(t *testing.T) {
	t.Parallel()
	withTrueColorProfile(t)

	prefix := styles.Secondary.Render("container | ")
	prefixWidth := lipgloss.Width(prefix)

	got := stripANSI(prefix + renderLogLine("abcdefghijklmnop", output.LogLevelInfo, 20-prefixWidth, prefixWidth))
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != "container | abcdefgh" {
		t.Fatalf("unexpected first line: %q", lines[0])
	}
	if lines[1] != "            ijklmnop" {
		t.Fatalf("unexpected continuation line: %q", lines[1])
	}
}

func TestRenderLogLineWrapsMetaAndMessage(t *testing.T) {
	t.Parallel()
	withTrueColorProfile(t)

	line := "abcd : WXYZ"
	got := renderLogLine(line, output.LogLevelWarn, 4, 0)
	want := wrap.HardWrap(line, 4)

	if stripANSI(got) != want {
		t.Fatalf("expected %q, got %q", want, stripANSI(got))
	}

	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 wrapped lines, got %d: %q", len(lines), got)
	}
	if lines[0] != styles.Secondary.Render("abcd") {
		t.Fatalf("unexpected first line styling: %q", lines[0])
	}
	if lines[1] != styles.Secondary.Render(" : ")+styles.Warning.Render("W") {
		t.Fatalf("unexpected mixed meta/message styling: %q", lines[1])
	}
	if lines[2] != styles.Warning.Render("XYZ") {
		t.Fatalf("unexpected final message styling: %q", lines[2])
	}
}

func TestRenderLogLineIndentsContinuationLines(t *testing.T) {
	t.Parallel()
	withTrueColorProfile(t)

	got := stripANSI(renderLogLine("abcdefghij", output.LogLevelInfo, 4, 3))
	want := "abcd\n   efgh\n   ij"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRenderLogLineZeroWidthPassesThrough(t *testing.T) {
	t.Parallel()
	withTrueColorProfile(t)

	got := stripANSI(renderLogLine("meta : payload", output.LogLevelWarn, 0, 12))
	want := "meta : payload"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("expected zero-width passthrough, got %q", got)
	}
}
