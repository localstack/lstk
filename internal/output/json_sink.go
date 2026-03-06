package output

import (
	"io"
	"os"
)

type JSONSink struct {
	out io.Writer
	err error
}

func NewJSONSink(out io.Writer) *JSONSink {
	if out == nil {
		out = os.Stdout
	}
	return &JSONSink{out: out}
}

// Err returns the first write error encountered, if any.
func (s *JSONSink) Err() error {
	return s.err
}

func (s *JSONSink) setErr(err error) {
	if s.err == nil && err != nil {
		s.err = err
	}
}

func (s *JSONSink) emit(event any) {
	b, ok := FormatEventJSON(event)
	if !ok {
		return
	}
	b = append(b, '\n')
	_, err := s.out.Write(b)
	s.setErr(err)
}
