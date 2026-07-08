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
// follow-logs goroutine writing into it cannot fail the capture. It trims
// before appending so a single oversized write never grows buf past maxBytes.
func (l *logTail) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(p) >= l.maxBytes {
		l.buf = append(l.buf[:0], p[len(p)-l.maxBytes:]...)
		return len(p), nil
	}

	if keep := l.maxBytes - len(p); len(l.buf) > keep {
		l.buf = l.buf[len(l.buf)-keep:]
	}
	l.buf = append(l.buf, p...)
	return len(p), nil
}

func (l *logTail) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.buf)
}
