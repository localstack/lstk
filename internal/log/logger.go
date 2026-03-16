package log

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Logger provides internal diagnostic logging.
// This is not for user-facing output — use output.Sink for that.
type Logger interface {
	Info(msg string, args ...any)
	Error(msg string, args ...any)
}

type logger struct {
	mu  sync.Mutex
	out io.Writer
}

// New creates a Logger that writes to out.
func New(out io.Writer) Logger {
	return &logger{out: out}
}

func (l *logger) Info(msg string, args ...any) {
	l.write("INFO", msg, args...)
}

func (l *logger) Error(msg string, args ...any) {
	l.write("ERROR", msg, args...)
}

func (l *logger) write(level, msg string, args ...any) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	l.mu.Lock()
	_, _ = fmt.Fprintf(l.out, "%s [%s] "+msg+"\n", append([]any{ts, level}, args...)...)
	l.mu.Unlock()
}

// Nop returns a logger that discards all messages.
func Nop() Logger {
	return nopLogger{}
}

type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
