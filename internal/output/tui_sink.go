package output

// Sender is implemented by bubbletea Program and test doubles.
type Sender interface {
	Send(msg any)
}

type TUISink struct {
	sender Sender
}

func NewTUISink(sender Sender) *TUISink {
	return &TUISink{sender: sender}
}

func (s *TUISink) emit(event any) {
	if s == nil || s.sender == nil {
		return
	}
	s.sender.Send(event)
}
