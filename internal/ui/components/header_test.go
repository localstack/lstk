package components

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"gotest.tools/v3/golden"
)

func TestHeaderView(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(original) })

	view := NewHeader("v1.0.0").View()
	golden.Assert(t, view, "header.golden")
}
