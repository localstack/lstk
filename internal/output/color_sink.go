package output

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// ColorSink emits events like PlainSink but renders ErrorEvent with ANSI color
// when the output is a TTY and NO_COLOR is not set.
type ColorSink struct {
	out     io.Writer
	colored bool
	err     error
}

// NewColorSink returns a ColorSink. Color is enabled only when out is a real
// terminal and the NO_COLOR environment variable is unset.
func NewColorSink(out io.Writer) *ColorSink {
	if out == nil {
		out = os.Stdout
	}
	return &ColorSink{out: out, colored: isColorTerminal(out)}
}

func (s *ColorSink) Err() error {
	return s.err
}

func (s *ColorSink) setErr(err error) {
	if s.err == nil && err != nil {
		s.err = err
	}
}

func (s *ColorSink) emit(event any) {
	var (
		line string
		ok   bool
	)
	if s.colored {
		line, ok = FormatColorEventLine(event)
	} else {
		line, ok = FormatEventLine(event)
	}
	if !ok {
		return
	}
	_, err := fmt.Fprintln(s.out, line)
	s.setErr(err)
}

// isColorTerminal reports whether w is a TTY and NO_COLOR is unset.
func isColorTerminal(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
