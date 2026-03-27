package awscli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var dotFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// ANSI color codes matching lstk's spinner style (color 69 blue) and secondary (color 241 gray)
const (
	spinnerColor   = "\033[38;5;69m"
	secondaryColor = "\033[38;5;241m"
	resetColor     = "\033[0m"
)

type spinner struct {
	out   io.Writer
	label string
	stop  chan struct{}
	done  chan struct{}
	mu    sync.Mutex
}

func newSpinner(out io.Writer, label string) *spinner {
	return &spinner{
		out:   out,
		label: label,
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (s *spinner) Start() {
	go func() {
		defer close(s.done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()

		i := 0
		for {
			s.mu.Lock()
			_, _ = fmt.Fprintf(s.out, "\r%s%s%s %s%s%s", spinnerColor, dotFrames[i%len(dotFrames)], resetColor, secondaryColor, s.label, resetColor)
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

func (s *spinner) Stop() {
	close(s.stop)
	<-s.done
}

func (s *spinner) clearLine() {
	s.mu.Lock()
	defer s.mu.Unlock()
	width := len(s.label) + 10
	_, _ = fmt.Fprintf(s.out, "\r%s\r", strings.Repeat(" ", width))
}

// isTerminal returns true if the writer is a terminal
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}
