package output

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var (
	secondaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	cautionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // used for Note and Warning
)

type PlainSink struct {
	out io.Writer
	err error
}

func NewPlainSink(out io.Writer) *PlainSink {
	if out == nil {
		out = os.Stdout
	}
	return &PlainSink{out: out}
}

// Err returns the first write error encountered, if any.
func (s *PlainSink) Err() error {
	return s.err
}

func (s *PlainSink) setErr(err error) {
	if s.err == nil && err != nil {
		s.err = err
	}
}

func (s *PlainSink) emit(event any) {
	var line string
	var ok bool

	switch e := event.(type) {
	case SuccessEvent:
		line = secondaryStyle.Render("> ") + successStyle.Render("Success:") + " " + e.Message
		ok = true
	case NoteEvent:
		line = secondaryStyle.Render("> ") + cautionStyle.Render("Note:") + " " + e.Message
		ok = true
	case WarningEvent:
		line = secondaryStyle.Render("> ") + cautionStyle.Render("Warning:") + " " + e.Message
		ok = true
	default:
		line, ok = FormatEventLine(event)
	}

	if !ok {
		return
	}

	_, err := fmt.Fprintln(s.out, line)
	s.setErr(err)
}
