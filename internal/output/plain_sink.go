package output

import (
	"fmt"
	"io"
	"os"
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
	line, ok := FormatEventLine(event)
	if !ok {
		return
	}
	_, err := fmt.Fprintln(s.out, line)
	s.setErr(err)
}
