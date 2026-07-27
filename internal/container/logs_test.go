package container

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/localstack/lstk/internal/config"
	"github.com/localstack/lstk/internal/output"
	"github.com/localstack/lstk/internal/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// captureSink records every LogLineEvent it receives.
type captureSink struct {
	mu    sync.Mutex
	lines []output.LogLineEvent
}

func (s *captureSink) Emit(e output.Event) {
	if le, ok := e.(output.LogLineEvent); ok {
		s.mu.Lock()
		s.lines = append(s.lines, le)
		s.mu.Unlock()
	}
}

func runLogs(t *testing.T, content string, verbose bool) *captureSink {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().
		StreamLogs(gomock.Any(), "localstack-aws", gomock.Any(), false, "all").
		DoAndReturn(func(_ context.Context, _ string, out io.Writer, _ bool, _ string) error {
			_, err := io.WriteString(out, content)
			return err
		})

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	err := Logs(context.Background(), mockRT, sink, containers, false, "all", verbose)
	require.NoError(t, err)
	return sink
}

// A single log line longer than maxLogLineBytes must not error (regression for
// "bufio.Scanner: token too long") and must be emitted truncated, not buffered
// whole in memory.
func TestLogs_LongLineIsTruncatedNotErrored(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", maxLogLineBytes+50_000)
	line := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : " + huge + "\n"

	sink := runLogs(t, line, false)

	require.Len(t, sink.lines, 1)
	emitted := sink.lines[0].Line
	assert.LessOrEqual(t, len(emitted), maxLogLineBytes+64, "emitted line must be bounded near the cap")
	assert.Contains(t, emitted, "truncated")
	assert.Equal(t, output.LogLevelInfo, sink.lines[0].Level, "level still parsed from the prefix")
}

func TestLogs_NormalLinesPassThrough(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : hello\n" +
		"2026-07-07T10:05:12.240  WARN --- [  MainThread] l.bar : world\n"

	sink := runLogs(t, content, false)

	require.Len(t, sink.lines, 2)
	assert.Equal(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : hello", sink.lines[0].Line)
	assert.Equal(t, output.LogLevelInfo, sink.lines[0].Level)
	assert.Equal(t, output.LogLevelWarn, sink.lines[1].Level)
}

// A final line without a trailing newline must still be emitted.
func TestLogs_NoTrailingNewline(t *testing.T) {
	t.Parallel()
	sink := runLogs(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail", false)

	require.Len(t, sink.lines, 1)
	assert.Equal(t, "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : tail", sink.lines[0].Line)
}

func TestLogs_FilteredLinesDropped(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] localstack.request.http : noise\n" +
		"2026-07-07T10:05:12.240  INFO --- [  MainThread] l.foo : keep\n"

	sink := runLogs(t, content, false)

	require.Len(t, sink.lines, 1)
	assert.Contains(t, sink.lines[0].Line, "keep")
}

// dockerTail mimics the runtime's Tail semantics — return the last n lines of
// the container's history — so tail tests exercise the same slicing order the
// real runtime applies.
func dockerTail(content, tail string) string {
	if tail == tailAll {
		return content
	}
	n, err := strconv.Atoi(tail)
	if err != nil {
		return content
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if n > len(lines) {
		n = len(lines)
	}
	if n == 0 {
		return ""
	}
	return strings.Join(lines[len(lines)-n:], "\n") + "\n"
}

func runLogsWithTail(t *testing.T, content, tail string) *captureSink {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().
		StreamLogs(gomock.Any(), "localstack-aws", gomock.Any(), false, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, out io.Writer, _ bool, tail string) error {
			_, err := io.WriteString(out, dockerTail(content, tail))
			return err
		}).AnyTimes()

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	err := Logs(context.Background(), mockRT, sink, containers, false, tail, false)
	require.NoError(t, err)
	return sink
}

func filteredLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "2026-07-07T10:05:%02d.240  INFO --- [et.reactor-0] localstack.request.http : GET /_localstack/health => 200\n", i)
	}
	return b.String()
}

// --tail counts the lines lstk prints, not the raw container lines. Letting the
// runtime apply the limit made `--tail 1` print nothing whenever the newest raw
// lines were ones the filter drops.
func TestLogs_TailCountsVisibleLinesNotFilteredOnes(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : keep\n" + filteredLines(5)

	sink := runLogsWithTail(t, content, "1")

	require.Len(t, sink.lines, 1, "the newest visible line must survive a --tail smaller than the filtered burst")
	assert.Contains(t, sink.lines[0].Line, "keep")
}

// When the first over-fetched suffix is entirely filtered, the fetch must grow
// until it reaches a visible line rather than reporting an empty tail.
func TestLogs_TailGrowsFetchPastLongFilteredRun(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : keep\n" + filteredLines(20)

	sink := runLogsWithTail(t, content, "1")

	require.Len(t, sink.lines, 1)
	assert.Contains(t, sink.lines[0].Line, "keep")
}

// A --tail larger than the number of visible lines shows all of them, and the
// order stays oldest-first.
func TestLogs_TailKeepsOrderAndCapsAtVisibleCount(t *testing.T) {
	t.Parallel()
	content := "2026-07-07T10:05:11.240  INFO --- [  MainThread] l.foo : first\n" +
		filteredLines(3) +
		"2026-07-07T10:05:12.240  INFO --- [  MainThread] l.foo : second\n" +
		"2026-07-07T10:05:13.240  INFO --- [  MainThread] l.foo : third\n"

	sink := runLogsWithTail(t, content, "2")
	require.Len(t, sink.lines, 2)
	assert.Contains(t, sink.lines[0].Line, "second")
	assert.Contains(t, sink.lines[1].Line, "third")

	all := runLogsWithTail(t, content, "10")
	require.Len(t, all.lines, 3, "a limit above the visible count shows every visible line")
	assert.Contains(t, all.lines[0].Line, "first")
}

// --tail 0 shows nothing and must not read the container's history to find out.
func TestLogs_TailZeroEmitsNothing(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	require.NoError(t, Logs(context.Background(), mockRT, sink, containers, false, "0", false))
	assert.Empty(t, sink.lines)
}

// Verbose mode prints every line, so the runtime's own tail is already exact
// and lstk must delegate to it instead of re-reading the history.
func TestLogs_VerboseDelegatesTailToRuntime(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(true, nil)
	mockRT.EXPECT().
		StreamLogs(gomock.Any(), "localstack-aws", gomock.Any(), false, "2").
		DoAndReturn(func(_ context.Context, _ string, out io.Writer, _ bool, tail string) error {
			_, err := io.WriteString(out, dockerTail(filteredLines(5), tail))
			return err
		})

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	require.NoError(t, Logs(context.Background(), mockRT, sink, containers, false, "2", true))
	assert.Len(t, sink.lines, 2, "verbose keeps filtered lines, so the runtime tail is exact")
}

// When no matching emulator container is running, Logs must fail with a
// silent error instead of trying to stream from a nonexistent container.
func TestLogs_EmulatorNotRunning_ReturnsSilentError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	mockRT := runtime.NewMockRuntime(ctrl)
	mockRT.EXPECT().IsHealthy(gomock.Any()).Return(nil)
	mockRT.EXPECT().IsRunning(gomock.Any(), "localstack-aws").Return(false, nil)
	mockRT.EXPECT().
		FindRunningByImage(gomock.Any(), []string{"localstack/localstack-pro", "localstack/localstack"}, "4566/tcp").
		Return(nil, nil)

	sink := &captureSink{}
	containers := []config.ContainerConfig{{Type: config.EmulatorAWS}}
	err := Logs(context.Background(), mockRT, sink, containers, false, "all", false)

	require.Error(t, err)
	assert.True(t, output.IsSilent(err), "error must be silent since the sink already surfaced a 'not running' message to the user")
}
