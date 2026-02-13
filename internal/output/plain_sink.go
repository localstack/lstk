package output

import (
	"fmt"
	"io"
	"os"
)

type PlainSink struct {
	out io.Writer
}

func NewPlainSink(out io.Writer) *PlainSink {
	if out == nil {
		out = os.Stdout
	}
	return &PlainSink{out: out}
}

func (s *PlainSink) emit(event any) {
	switch e := event.(type) {
	case LogEvent:
		_, _ = fmt.Fprintln(s.out, e.Message)
	case WarningEvent:
		_, _ = fmt.Fprintf(s.out, "Warning: %s\n", e.Message)
	case ContainerStatusEvent:
		s.emitStatus(e)
	case ProgressEvent:
		s.emitProgress(e)
	}
}

func (s *PlainSink) emitStatus(e ContainerStatusEvent) {
	switch e.Phase {
	case "pulling":
		_, _ = fmt.Fprintf(s.out, "Pulling %s...\n", e.Container)
	case "starting":
		_, _ = fmt.Fprintf(s.out, "Starting %s...\n", e.Container)
	case "waiting":
		_, _ = fmt.Fprintf(s.out, "Waiting for %s to be ready...\n", e.Container)
	case "ready":
		if e.Detail != "" {
			_, _ = fmt.Fprintf(s.out, "%s ready (%s)\n", e.Container, e.Detail)
		} else {
			_, _ = fmt.Fprintf(s.out, "%s ready\n", e.Container)
		}
	default:
		if e.Detail != "" {
			_, _ = fmt.Fprintf(s.out, "%s: %s (%s)\n", e.Container, e.Phase, e.Detail)
		} else {
			_, _ = fmt.Fprintf(s.out, "%s: %s\n", e.Container, e.Phase)
		}
	}
}

func (s *PlainSink) emitProgress(e ProgressEvent) {
	if e.Total > 0 {
		pct := float64(e.Current) / float64(e.Total) * 100
		_, _ = fmt.Fprintf(s.out, "  %s: %s %.1f%%\n", e.LayerID, e.Status, pct)
	} else if e.Status != "" {
		_, _ = fmt.Fprintf(s.out, "  %s: %s\n", e.LayerID, e.Status)
	}
}
