package container

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
)

// maxLogLineBytes caps how much of a single log line lstk buffers before
// emitting it. Anything beyond this is discarded (with a marker) rather than
// held in memory, so a pathologically long line (e.g. a CDK CloudFormation
// template dumped as one line in a request body) can neither exhaust memory nor
// overflow a fixed scanner buffer.
const maxLogLineBytes = 256 * 1024

// logReaderBufferSize is the bufio.Reader buffer size; larger than the default
// so each ReadSlice call returns bigger chunks when draining a long line.
const logReaderBufferSize = 64 * 1024

func Logs(ctx context.Context, rt runtime.Runtime, sink output.Sink, containers []config.ContainerConfig, follow bool, verbose bool) error {
	if err := rt.IsHealthy(ctx); err != nil {
		rt.EmitUnhealthyError(sink, err)
		return output.NewSilentError(fmt.Errorf("runtime not healthy: %w", err))
	}

	if len(containers) == 0 {
		return fmt.Errorf("no containers configured")
	}

	// TODO: handle logs per container
	c := containers[0]

	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := rt.StreamLogs(ctx, c.Name(), pw, follow)
		pw.CloseWithError(err)
		errCh <- err
	}()

	reader := bufio.NewReaderSize(pr, logReaderBufferSize)
	for {
		line, truncated, ok, err := readBoundedLine(reader, maxLogLineBytes)
		if ok {
			if verbose || !shouldFilter(line) {
				level, _ := parseLogLine(line)
				if truncated > 0 {
					line = fmt.Sprintf("%s … (%d more bytes truncated)", line, truncated)
				}
				sink.Emit(output.LogLineEvent{Source: output.LogSourceEmulator, Line: line, Level: level})
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			if ctx.Err() != nil {
				break
			}
			return err
		}
	}
	return <-errCh
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
