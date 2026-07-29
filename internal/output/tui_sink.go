package output

// Sender abstracts Bubble Tea's Program.Send to keep TUISink decoupled and testable.
type Sender interface {
	Send(msg any)
}

// LogLinePrinter abstracts Bubble Tea's Program.Println — permanent output
// that persists across renders, unlike a Sender's capped/repainted lines.
type LogLinePrinter interface {
	PrintLogLine(event LogLineEvent)
}

type TUISink struct {
	sender     Sender
	logPrinter LogLinePrinter
}

func NewTUISink(sender Sender) *TUISink {
	return &TUISink{sender: sender}
}

// NewStreamingLogSink is like NewTUISink but routes LogLineEvent through
// logPrinter instead of sender, giving log lines real terminal scrollback
// instead of the TUI model's capped/repainted history.
func NewStreamingLogSink(sender Sender, logPrinter LogLinePrinter) *TUISink {
	return &TUISink{sender: sender, logPrinter: logPrinter}
}

func (s *TUISink) Emit(event Event) {
	if s == nil {
		return
	}
	if s.logPrinter != nil {
		if logLine, ok := event.(LogLineEvent); ok {
			s.logPrinter.PrintLogLine(logLine)
			return
		}
	}
	if s.sender == nil {
		return
	}
	s.sender.Send(event)
}
