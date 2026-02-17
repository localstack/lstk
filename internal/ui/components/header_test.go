package components

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"gotest.tools/v3/golden"
)

func TestHeaderView(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	view := NewHeader("v1.0.0").View()
	golden.Assert(t, view, "header.golden")
}
