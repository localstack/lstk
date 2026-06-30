package container

import "sync"

// logTail is a thread-safe, byte-bounded buffer that retains the most recent
// maxBytes written to it. It captures a container's startup logs while the
// container runs so they survive the container being auto-removed (--rm) the
// instant it exits — otherwise a crash during startup would leave no diagnostics.
type logTail struct {
	mu       sync.Mutex
	buf      []byte
	maxBytes int
}

func newLogTail(maxBytes int) *logTail {
	return &logTail{maxBytes: maxBytes}
}

// Write appends p, keeping only the most recent maxBytes. It never errors, so a
// follow-logs goroutine writing into it cannot fail the capture.
func (l *logTail) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	if len(l.buf) > l.maxBytes {
		trimmed := make([]byte, l.maxBytes)
		copy(trimmed, l.buf[len(l.buf)-l.maxBytes:])
		l.buf = trimmed
	}
	return len(p), nil
}

func (l *logTail) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}
