package output

import (
	"fmt"
	"io"
	"os"
)

type PlainSink struct {
	out    io.Writer
	errOut io.Writer
	err    error
}

func NewPlainSink(out io.Writer) *PlainSink {
	if out == nil {
		out = os.Stdout
	}
	return &PlainSink{out: out, errOut: out}
}

// NewPlainSinkSplit creates a PlainSink that routes ErrorEvents to errOut and all others to out.
func NewPlainSinkSplit(out, errOut io.Writer) *PlainSink {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	return &PlainSink{out: out, errOut: errOut}
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

func (s *PlainSink) Emit(event Event) {
	line, ok := FormatEventLine(event)
	if !ok {
		return
	}
	w := s.out
	if _, isErr := event.(ErrorEvent); isErr {
		w = s.errOut
	}
	_, err := fmt.Fprintln(w, line)
	s.setErr(err)
}
