package container

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// maxLogLineBytes caps how much of a single log line lstk emits before
// truncating it (with a marker). Anything beyond this is drained and discarded
// rather than held in memory, so a pathologically long line (e.g. a CDK
// CloudFormation template or a binary asset upload dumped as one line in a
// request body) can neither exhaust memory nor overflow a fixed scanner buffer.
// Kept deliberately small so a single oversized line stays readable in a
// terminal instead of flooding the scrollback with hundreds of KB.
const maxLogLineBytes = 8 * 1024

// logReaderBufferSize is the bufio.Reader buffer size, larger than the default
// so each ReadSlice call returns bigger chunks — this speeds up draining the
// discarded tail of a very long line (independent of maxLogLineBytes, which
// bounds only what is kept and emitted).
const logReaderBufferSize = 64 * 1024

// tailAll is the --tail value meaning "no limit".
const tailAll = "all"

// backlogFetchFactor over-fetches raw container lines relative to the requested
// --tail, so the filter usually has enough survivors after a single pass.
const backlogFetchFactor = 8

// maxBacklogFetch bounds that over-fetch; beyond it lstk reads the container's
// whole history rather than growing the request further.
const maxBacklogFetch = 100_000

func Logs(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, follow bool, tail string, verbose bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	if len(containers) == 0 {
		return fmt.Errorf("no containers configured")
	}

	// TODO: handle logs per container
	c := containers[0]

	name, err := ResolveRunningContainerName(ctx, rt, c)
	if err != nil {
		return fmt.Errorf("checking %s running: %w", c.Name(), err)
	}
	if name == "" {
		return HandleNoRunningContainer(sink, c)
	}

	emit := func(line string, level output.LogLevel) {
		sink.Emit(output.LogLineEvent{Source: output.LogSourceEmulator, Line: line, Level: level})
	}

	// A --tail limit counts the lines lstk prints, not the raw container lines.
	// Letting the runtime apply the limit would count lines that the filter
	// then drops, so `--tail 1` prints nothing whenever the newest raw line
	// happens to be a filtered one. Verbose mode prints every line, so there
	// the runtime's own tail is already exact (and far cheaper).
	limit, hasLimit := parseTailLimit(tail)
	if !hasLimit || verbose {
		_, err := forEachLogLine(ctx, rt, name, follow, tail, verbose, emit)
		return err
	}

	if err := emitFilteredBacklog(ctx, rt, name, limit, emit); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	// The backlog is already printed, so stream only what arrives from here on.
	// Lines written in the gap between the two calls are not shown.
	_, err = forEachLogLine(ctx, rt, name, true, "0", verbose, emit)
	return err
}

// parseTailLimit reports the numeric --tail limit, if the value sets one.
func parseTailLimit(tail string) (int, bool) {
	if tail == "" || tail == tailAll {
		return 0, false
	}
	n, err := strconv.Atoi(tail)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// emitFilteredBacklog emits the last limit lines that survive filtering.
func emitFilteredBacklog(ctx context.Context, rt runtime.Runtime, name string, limit int, emit func(string, output.LogLevel)) error {
	if limit == 0 {
		return nil
	}

	// Filtering is per-line and order-preserving, so the last limit survivors
	// of a long enough raw suffix are the last limit survivors overall. Grow
	// the suffix only when it turned out to be too heavily filtered, so a small
	// --tail on a long-running emulator does not stream the whole history.
	ring := newLineRing(limit)
	for fetch := limit * backlogFetchFactor; ; fetch *= backlogFetchFactor {
		unbounded := fetch <= 0 || fetch > maxBacklogFetch // also covers int overflow
		tailArg := tailAll
		if !unbounded {
			tailArg = strconv.Itoa(fetch)
		}

		ring.reset()
		raw, err := forEachLogLine(ctx, rt, name, false, tailArg, false, ring.add)
		if err != nil {
			return err
		}
		// A short read means the suffix covered the whole history.
		if unbounded || ring.len() == limit || raw < fetch {
			break
		}
	}
	ring.emit(emit)
	return nil
}

// forEachLogLine streams the container's logs, calling fn for every line that
// survives filtering, and reports how many raw lines it read.
func forEachLogLine(ctx context.Context, rt runtime.Runtime, name string, follow bool, tail string, verbose bool, fn func(string, output.LogLevel)) (int, error) {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		// Deferred so that a goroutine death which skips the normal return —
		// a panic, or a test double calling t.Fatal — still closes the pipe and
		// reports back. Otherwise the read loop below blocks on a writer that
		// will never write, and the errCh receive never completes.
		var err error
		defer func() {
			pw.CloseWithError(err)
			errCh <- err
		}()
		err = rt.StreamLogs(ctx, name, pw, follow, tail)
	}()

	raw := 0
	reader := bufio.NewReaderSize(pr, logReaderBufferSize)
	for {
		line, truncated, ok, err := readBoundedLine(reader, maxLogLineBytes)
		if ok {
			raw++
			if verbose || !shouldFilter(line) {
				level, _ := parseLogLine(line)
				if truncated > 0 {
					line = fmt.Sprintf("%s … (%d more bytes truncated)", line, truncated)
				}
				fn(line, level)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if ctx.Err() != nil {
				break
			}
			return raw, err
		}
	}
	return raw, <-errCh
}

// lineRing keeps the most recent n log lines, overwriting the oldest.
type lineRing struct {
	buf  []logLine
	next int
	full bool
}

type logLine struct {
	text  string
	level output.LogLevel
}

func newLineRing(n int) *lineRing { return &lineRing{buf: make([]logLine, n)} }

func (r *lineRing) add(text string, level output.LogLevel) {
	if len(r.buf) == 0 {
		return
	}
	r.buf[r.next] = logLine{text: text, level: level}
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

func (r *lineRing) len() int {
	if r.full {
		return len(r.buf)
	}
	return r.next
}

func (r *lineRing) reset() {
	r.next, r.full = 0, false
}

func (r *lineRing) emit(fn func(string, output.LogLevel)) {
	start := 0
	if r.full {
		start = r.next
	}
	for i := range r.len() {
		l := r.buf[(start+i)%len(r.buf)]
		fn(l.text, l.level)
	}
}

// readBoundedLine reads one newline-delimited line from r, buffering at most max
// bytes of content. Bytes beyond max are drained and discarded (counted in
// truncated) so memory stays bounded regardless of line length. The returned
// line has its trailing newline (and any preceding carriage return) stripped.
// ok is false only when nothing was read (e.g. clean EOF at a line boundary).
func readBoundedLine(r *bufio.Reader, max int) (line string, truncated int, ok bool, err error) {
	var buf []byte
	for {
		var chunk []byte
		chunk, err = r.ReadSlice('\n')
		if err == nil {
			chunk = trimEOL(chunk) // only the terminating chunk carries the newline
		}
		ok = ok || len(chunk) > 0 || err == nil
		if room := max - len(buf); room > 0 {
			if len(chunk) <= room {
				buf = append(buf, chunk...)
			} else {
				buf = append(buf, chunk[:room]...)
				truncated += len(chunk) - room
			}
		} else {
			truncated += len(chunk)
		}
		if err == bufio.ErrBufferFull {
			continue // line longer than the reader buffer; keep draining to the newline
		}
		return string(buf), truncated, ok, err
	}
}

func trimEOL(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
		if n = len(b); n > 0 && b[n-1] == '\r' {
			b = b[:n-1]
		}
	}
	return b
}
