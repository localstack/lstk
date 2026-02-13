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
	switch e := event.(type) {
	case LogEvent:
		_, err := fmt.Fprintln(s.out, e.Message)
		s.setErr(err)
	case WarningEvent:
		_, err := fmt.Fprintf(s.out, "Warning: %s\n", e.Message)
		s.setErr(err)
	case ContainerStatusEvent:
		s.emitStatus(e)
	case ProgressEvent:
		s.emitProgress(e)
	}
}

func (s *PlainSink) emitStatus(e ContainerStatusEvent) {
	var err error
	switch e.Phase {
	case "pulling":
		_, err = fmt.Fprintf(s.out, "Pulling %s...\n", e.Container)
	case "starting":
		_, err = fmt.Fprintf(s.out, "Starting %s...\n", e.Container)
	case "waiting":
		_, err = fmt.Fprintf(s.out, "Waiting for %s to be ready...\n", e.Container)
	case "ready":
		if e.Detail != "" {
			_, err = fmt.Fprintf(s.out, "%s ready (%s)\n", e.Container, e.Detail)
		} else {
			_, err = fmt.Fprintf(s.out, "%s ready\n", e.Container)
		}
	default:
		if e.Detail != "" {
			_, err = fmt.Fprintf(s.out, "%s: %s (%s)\n", e.Container, e.Phase, e.Detail)
		} else {
			_, err = fmt.Fprintf(s.out, "%s: %s\n", e.Container, e.Phase)
		}
	}
	s.setErr(err)
}

func (s *PlainSink) emitProgress(e ProgressEvent) {
	var err error
	if e.Total > 0 {
		pct := float64(e.Current) / float64(e.Total) * 100
		_, err = fmt.Fprintf(s.out, "  %s: %s %.1f%%\n", e.LayerID, e.Status, pct)
	} else if e.Status != "" {
		_, err = fmt.Fprintf(s.out, "  %s: %s\n", e.LayerID, e.Status)
	}
	s.setErr(err)
}
