package terminal

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

var dotFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// ANSI color codes matching the lstk style palette (color 69 = Nimbo blue, color 241 = secondary gray).
const (
	spinnerColor   = "\033[38;5;69m"
	secondaryColor = "\033[38;5;241m"
	resetColor     = "\033[0m"
)

type Spinner struct {
	out      io.Writer
	label    string
	stop     chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	stopOnce sync.Once
}

func NewSpinner(out io.Writer, label string) *Spinner {
	return &Spinner{
		out:   out,
		label: label,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (s *Spinner) Start() {
	go func() {
		defer close(s.done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()

		i := 0
		for {
			s.mu.Lock()
			_, _ = fmt.Fprintf(s.out, "\r\033[2K%s%s%s %s%s%s", spinnerColor, dotFrames[i%len(dotFrames)], resetColor, secondaryColor, s.label, resetColor)
			s.mu.Unlock()

			select {
			case <-s.stop:
				s.clearLine()
				return
			case <-tick.C:
				i++
			}
		}
	}()
}

func (s *Spinner) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
	})
	<-s.done
}

func (s *Spinner) clearLine() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = fmt.Fprint(s.out, "\r\033[2K")
}

// IsTerminal reports whether w is a terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// StopOnWriteWriter wraps a writer and stops the spinner on the first write.
type StopOnWriteWriter struct {
	W       io.Writer
	Spinner *Spinner
	once    sync.Once
}

func (s *StopOnWriteWriter) Write(p []byte) (int, error) {
	s.once.Do(func() {
		s.Spinner.Stop()
	})
	return s.W.Write(p)
}
