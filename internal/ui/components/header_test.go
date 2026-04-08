package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"gotest.tools/v3/golden"
)

func TestHeaderView(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	view := NewHeader("v1.0.0", "LocalStack AWS Emulator", "~/.config/lstk/config.toml").View()
	golden.Assert(t, view, "header.golden")
}

func TestHeaderViewWithPlanName(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	view := NewHeader("v1.0.0", "LocalStack Ultimate", "~/.config/lstk/config.toml").View()
	if !strings.Contains(view, "LocalStack Ultimate") {
		t.Fatal("expected plan name in header view")
	}
}

func TestHeaderViewWithoutConfigPath(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	withPath := NewHeader("v1.0.0", "LocalStack AWS Emulator", "~/.config/lstk/config.toml").View()
	withoutPath := NewHeader("v1.0.0", "LocalStack AWS Emulator", "").View()

	if !strings.Contains(withPath, "~/.config/lstk/config.toml") {
		t.Fatal("expected config path in header view when provided")
	}
	if strings.Contains(withoutPath, "~/.config/lstk/config.toml") {
		t.Fatal("expected no config path in header view when empty")
	}
}
